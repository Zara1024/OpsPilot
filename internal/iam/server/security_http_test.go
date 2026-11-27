package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/iam/biz/membership"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/mfa"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/org"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/sso"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/throttle"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/user"
	membershipstore "github.com/Zara1024/OpsPilot/internal/iam/data/membership/store"
	orgstore "github.com/Zara1024/OpsPilot/internal/iam/data/org/store"
	securitystore "github.com/Zara1024/OpsPilot/internal/iam/data/security/store"
	userstore "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/iam/service"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
)

type iamSecurityHTTPFixture struct {
	router chi.Router
	svc    *service.Service
	signer *auth.Signer
	mfaSvc *mfa.Service
	ssoSvc *sso.Service
	userUC *user.Usecase
}

func newIAMSecurityHTTPFixture(t *testing.T) *iamSecurityHTTPFixture {
	t.Helper()
	t.Setenv("OPSPILOT_SECRET_KEY", "test-iam-http-security-secretbox-key")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "iam_http.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Org{}, &model.OrgMembership{}))
	require.NoError(t, securitystore.Migrate(db))

	signer := auth.NewSigner("test-jwt-secret-key-http-sec", 15*time.Minute, time.Hour)
	userRepo := userstore.NewRepo(db)
	userUC := user.NewUsecase(userRepo, signer, nil)
	secStore := securitystore.New(db)
	orgRepo := orgstore.NewRepo(db)
	msRepo := membershipstore.NewRepo(db)
	orgSvc := org.New(orgRepo, msRepo, nil)
	msSvc := membership.New(msRepo, nil)
	_, err = orgSvc.EnsureSeed(context.Background(), "默认组织", "default")
	require.NoError(t, err)

	throttleSvc := throttle.New(secStore)
	mfaSvc := mfa.New(secStore)
	ssoSvc := sso.New(secStore, userUC)
	ssoSvc.SetOrgWiring(msSvc, orgSvc)

	svc := service.New(userUC, nil)
	svc.SetOrgs(orgSvc)
	svc.SetMemberships(msSvc)
	svc.SetThrottle(throttleSvc)
	svc.SetMFA(mfaSvc)
	svc.SetSSO(ssoSvc)

	h := NewHandler(svc, nil)
	r := chi.NewRouter()
	h.RegisterPublic(r)
	r.Group(func(protected chi.Router) {
		protected.Use(auth.Middleware(signer))
		h.RegisterProtected(protected)
	})

	return &iamSecurityHTTPFixture{
		router: r,
		svc:    svc,
		signer: signer,
		mfaSvc: mfaSvc,
		ssoSvc: ssoSvc,
		userUC: userUC,
	}
}

func doJSONReq(t *testing.T, r http.Handler, method, path, bearer string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "opspilot.test"
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHTTP_MFAEndToEnd_LoginChallengeAndMandatoryEnroll(t *testing.T) {
	f := newIAMSecurityHTTPFixture(t)
	ctx := context.Background()

	require.NoError(t, f.userUC.BootstrapAdmin(ctx, "admin@test.com", "AdminPass123!"))
	_, err := f.userUC.Create(ctx, user.CreateInput{
		Email:    "dev@test.com",
		Password: "DevPassword123!",
		Role:     model.RoleUser,
	})
	require.NoError(t, err)

	// 1. 普通用户登录拿 access token
	w := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":    "dev@test.com",
		"password": "DevPassword123!",
	})
	require.Equal(t, http.StatusOK, w.Code)
	var devLogin loginResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &devLogin))
	require.NotEmpty(t, devLogin.AccessToken)
	require.False(t, devLogin.MFARequired)

	// 2. Refresh token 禁止访问受保护接口，Access token 禁止调用 /refresh
	wRefreshOnAPI := doJSONReq(t, f.router, http.MethodGet, "/v1/me", devLogin.RefreshToken, nil)
	require.Equal(t, http.StatusUnauthorized, wRefreshOnAPI.Code)
	wAccessOnRefresh := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/refresh", "", map[string]string{
		"refresh_token": devLogin.AccessToken,
	})
	require.Equal(t, http.StatusUnauthorized, wAccessOnRefresh.Code)

	// 3. 普通用户自助开启 MFA
	wSetup := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/mfa/setup", devLogin.AccessToken, nil)
	require.Equal(t, http.StatusOK, wSetup.Code)
	var setup mfa.SetupResult
	require.NoError(t, json.Unmarshal(wSetup.Body.Bytes(), &setup))
	require.NotEmpty(t, setup.Secret)

	codeNow, err := mfa.GenerateTOTP(setup.Secret, time.Now().UTC())
	require.NoError(t, err)
	wConfirm := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/mfa/confirm", devLogin.AccessToken, map[string]string{"code": codeNow})
	require.Equal(t, http.StatusOK, wConfirm.Code)
	var confirmOut struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(wConfirm.Body.Bytes(), &confirmOut))
	require.Len(t, confirmOut.RecoveryCodes, 8)

	// 4. 再次登录进入 MFA 挑战，使用一次性恢复码完成登录
	w2 := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":    "dev@test.com",
		"password": "DevPassword123!",
	})
	require.Equal(t, http.StatusOK, w2.Code)
	var mfaPrompt loginResp
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &mfaPrompt))
	require.True(t, mfaPrompt.MFARequired)
	require.Empty(t, mfaPrompt.AccessToken)
	require.NotEmpty(t, mfaPrompt.MFAToken)

	// mfa_token 不能直调业务 API
	wMfaOnAPI := doJSONReq(t, f.router, http.MethodGet, "/v1/me", mfaPrompt.MFAToken, nil)
	require.Equal(t, http.StatusUnauthorized, wMfaOnAPI.Code)

	wVerify := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/mfa/verify", "", map[string]string{
		"mfa_token": mfaPrompt.MFAToken,
		"code":      confirmOut.RecoveryCodes[0],
	})
	require.Equal(t, http.StatusOK, wVerify.Code)
	var verified loginResp
	require.NoError(t, json.Unmarshal(wVerify.Body.Bytes(), &verified))
	require.NotEmpty(t, verified.AccessToken)

	// 5. 管理员登录并将认证策略设为 admins 强制启用 MFA
	wAdmin := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":    "admin@test.com",
		"password": "AdminPass123!",
	})
	require.Equal(t, http.StatusOK, wAdmin.Code)
	var adminLogin loginResp
	require.NoError(t, json.Unmarshal(wAdmin.Body.Bytes(), &adminLogin))

	wPolicy := doJSONReq(t, f.router, http.MethodPut, "/v1/iam/auth-policy", adminLogin.AccessToken, map[string]any{
		"mfa_enforce_mode":           "admins",
		"allow_local_password_login": true,
		"login_ip_limit":             20,
		"login_ip_window_sec":        300,
		"login_email_limit":          6,
		"login_email_window_sec":     900,
	})
	require.Equal(t, http.StatusOK, wPolicy.Code)

	// 管理员再次登录必须收到 mfa_setup_required=true，并通过 enroll 接口就地完成绑定
	wAdminForce := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":    "admin@test.com",
		"password": "AdminPass123!",
	})
	require.Equal(t, http.StatusOK, wAdminForce.Code)
	var forceResp loginResp
	require.NoError(t, json.Unmarshal(wAdminForce.Body.Bytes(), &forceResp))
	require.True(t, forceResp.MFASetupRequired)
	require.NotEmpty(t, forceResp.MFAToken)

	wEnrollStart := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/mfa/enroll/start", "", map[string]string{
		"mfa_token": forceResp.MFAToken,
	})
	require.Equal(t, http.StatusOK, wEnrollStart.Code)
	var adminSetup mfa.SetupResult
	require.NoError(t, json.Unmarshal(wEnrollStart.Body.Bytes(), &adminSetup))

	adminCode, err := mfa.GenerateTOTP(adminSetup.Secret, time.Now().UTC())
	require.NoError(t, err)
	wEnrollDone := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/mfa/enroll/confirm", "", map[string]string{
		"mfa_token": forceResp.MFAToken,
		"code":      adminCode,
	})
	require.Equal(t, http.StatusOK, wEnrollDone.Code)
	var enrollDone struct {
		AccessToken   string   `json:"access_token"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	require.NoError(t, json.Unmarshal(wEnrollDone.Body.Bytes(), &enrollDone))
	require.NotEmpty(t, enrollDone.AccessToken)
	require.Len(t, enrollDone.RecoveryCodes, 8)
}

func TestHTTP_SSOProviderAdminCRUDAndLoginExchange(t *testing.T) {
	f := newIAMSecurityHTTPFixture(t)
	ctx := context.Background()

	require.NoError(t, f.userUC.BootstrapAdmin(ctx, "admin@test.com", "AdminPass123!"))
	wAdmin := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":    "admin@test.com",
		"password": "AdminPass123!",
	})
	var adminLogin loginResp
	require.NoError(t, json.Unmarshal(wAdmin.Body.Bytes(), &adminLogin))

	// Mock 飞书服务端完成 HTTP 层全链路登录测试
	feishuMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "access_token": "tok-1"})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"union_id":         "u-fs-99",
					"enterprise_email": "sso.user@corp.test",
					"name":             "SSO Test User",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer feishuMock.Close()

	// 普通用户禁止配置 SSO provider
	_, err := f.userUC.Create(ctx, user.CreateInput{Email: "u@test.com", Password: "UserPass123!", Role: model.RoleUser})
	require.NoError(t, err)
	wUserLogin := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email": "u@test.com", "password": "UserPass123!",
	})
	var userLogin loginResp
	require.NoError(t, json.Unmarshal(wUserLogin.Body.Bytes(), &userLogin))
	wForbidden := doJSONReq(t, f.router, http.MethodGet, "/v1/iam/sso/providers", userLogin.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, wForbidden.Code)

	// 管理员创建 Feishu SSO provider（返回不得泄露 client_secret）
	wCreate := doJSONReq(t, f.router, http.MethodPost, "/v1/iam/sso/providers", adminLogin.AccessToken, map[string]any{
		"slug":             "feishu-it",
		"name":             "飞书 SSO",
		"type":             "feishu",
		"enabled":          true,
		"auth_url":         feishuMock.URL + "/auth",
		"token_url":        feishuMock.URL + "/token",
		"userinfo_url":     feishuMock.URL + "/userinfo",
		"client_id":        "cli_123",
		"client_secret":    "super-secret-value",
		"auto_create_user": true,
		"default_role":     "user",
	})
	require.Equal(t, http.StatusCreated, wCreate.Code)
	require.NotContains(t, wCreate.Body.String(), "super-secret-value")
	require.Contains(t, wCreate.Body.String(), `"has_client_secret":true`)

	// 公开列表仅暴露安全摘要
	wPublic := doJSONReq(t, f.router, http.MethodGet, "/v1/auth/sso/providers", "", nil)
	require.Equal(t, http.StatusOK, wPublic.Code)
	require.Contains(t, wPublic.Body.String(), `"slug":"feishu-it"`)
	require.NotContains(t, wPublic.Body.String(), "super-secret-value")

	// 发起登录（mode=json 返回授权跳转地址与一次性 state）
	wStart := doJSONReq(t, f.router, http.MethodGet, "/v1/auth/sso/feishu-it/start?mode=json&redirect=/devices", "", nil)
	require.Equal(t, http.StatusOK, wStart.Code)
	var startOut struct {
		AuthorizeURL string `json:"authorize_url"`
		CallbackURL  string `json:"callback_url"`
	}
	require.NoError(t, json.Unmarshal(wStart.Body.Bytes(), &startOut))
	parsed, err := url.Parse(startOut.AuthorizeURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.True(t, strings.HasSuffix(startOut.CallbackURL, "/login/sso/callback/feishu-it"))

	// 回调换取 OpsPilot 会话 token
	wExch := doJSONReq(t, f.router, http.MethodPost, "/v1/auth/sso/feishu-it/exchange", "", map[string]string{
		"code":         "fs-auth-code",
		"state":        state,
		"redirect_uri": startOut.CallbackURL,
	})
	require.Equal(t, http.StatusOK, wExch.Code)
	var ssoLogin loginResp
	require.NoError(t, json.Unmarshal(wExch.Body.Bytes(), &ssoLogin))
	require.NotEmpty(t, ssoLogin.AccessToken)
	require.Equal(t, "sso.user@corp.test", ssoLogin.Email)
	require.Equal(t, "/devices", ssoLogin.RedirectTo)

	// 登录后查看自己绑定的外部身份
	wIdents := doJSONReq(t, f.router, http.MethodGet, "/v1/auth/identities", ssoLogin.AccessToken, nil)
	require.Equal(t, http.StatusOK, wIdents.Code)
	require.Contains(t, wIdents.Body.String(), "u-fs-99")
}
