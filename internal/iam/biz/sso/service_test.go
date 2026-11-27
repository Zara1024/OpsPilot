package sso

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	membershipstore "github.com/Zara1024/OpsPilot/internal/iam/data/membership/store"
	orgstore "github.com/Zara1024/OpsPilot/internal/iam/data/org/store"
	securitystore "github.com/Zara1024/OpsPilot/internal/iam/data/security/store"
	userstore "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/membership"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/org"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/user"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

type ssoTestHarness struct {
	svc    *Service
	secSt  *securitystore.Store
	orgSvc *org.Service
	msSvc  *membership.Service
	userUC *user.Usecase
}

func newSSOTestHarness(t *testing.T) *ssoTestHarness {
	t.Helper()
	t.Setenv("OPSPILOT_SECRET_KEY", "test-sso-secretbox-key-unit")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sso.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Org{}, &model.OrgMembership{}))
	require.NoError(t, securitystore.Migrate(db))

	secSt := securitystore.New(db)
	userRepo := userstore.NewRepo(db)
	signer := auth.NewSigner("jwt-test-secret", 15*time.Minute, time.Hour)
	userUC := user.NewUsecase(userRepo, signer, nil)
	orgRepo := orgstore.NewRepo(db)
	msRepo := membershipstore.NewRepo(db)
	orgSvc := org.New(orgRepo, msRepo, nil)
	msSvc := membership.New(msRepo, nil)

	_, err = orgSvc.EnsureSeed(context.Background(), "默认组织", "default")
	require.NoError(t, err)

	svc := New(secSt, userUC)
	svc.SetOrgWiring(msSvc, orgSvc)
	return &ssoTestHarness{svc: svc, secSt: secSt, orgSvc: orgSvc, msSvc: msSvc, userUC: userUC}
}

func TestOIDC_FullFlowWithPKCE_JWKS_JIT_AndReplayProtection(t *testing.T) {
	h := newSSOTestHarness(t)
	ctx := context.Background()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	var srvURL string
	var capturedChallenge string
	var capturedNonce string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 srvURL,
				"authorization_endpoint": srvURL + "/authorize",
				"token_endpoint":         srvURL + "/token",
				"userinfo_endpoint":      srvURL + "/userinfo",
				"jwks_uri":               srvURL + "/jwks",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA",
					"kid": "k1",
					"use": "sig",
					"alg": "RS256",
					"n":   base64.RawURLEncoding.EncodeToString(privKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.E)).Bytes()),
				}},
			})
		case "/token":
			require.NoError(t, r.ParseForm())
			require.Equal(t, "oidc-code-1", r.Form.Get("code"))
			require.Equal(t, "cid-oidc", r.Form.Get("client_id"))
			require.Equal(t, "sec-oidc", r.Form.Get("client_secret"))
			verifier := r.Form.Get("code_verifier")
			require.Equal(t, capturedChallenge, pkceS256(verifier))

			idClaims := jwt.MapClaims{
				"iss":   srvURL,
				"sub":   "ext-oidc-001",
				"aud":   "cid-oidc",
				"nonce": capturedNonce,
				"email": "alice@corp.example.com",
				"name":  "Alice Corp",
				"exp":   time.Now().Add(5 * time.Minute).Unix(),
			}
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, idClaims)
			tok.Header["kid"] = "k1"
			idToken, signErr := tok.SignedString(privKey)
			require.NoError(t, signErr)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "acc-tok",
				"id_token":     idToken,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	prov, err := h.svc.CreateProvider(ctx, ProviderInput{
		Slug:           "okta-prod",
		Name:           "Corp Okta",
		Type:           model.SSOTypeOIDC,
		Enabled:        true,
		IssuerURL:      srv.URL,
		ClientID:       "cid-oidc",
		ClientSecret:   "sec-oidc",
		AutoCreateUser: true,
		DefaultRole:    model.RoleUser,
		AllowedDomains: "corp.example.com",
	})
	require.NoError(t, err)
	require.NotEmpty(t, prov.ClientSecretEnc)
	require.NotEqual(t, "sec-oidc", prov.ClientSecretEnc)

	authURL, err := h.svc.StartLogin(ctx, "okta-prod", "https://opspilot.test/login/sso/callback?provider=okta-prod", "/devices")
	require.NoError(t, err)
	parsedAuth, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsedAuth.Query().Get("state")
	capturedChallenge = parsedAuth.Query().Get("code_challenge")
	capturedNonce = parsedAuth.Query().Get("nonce")
	require.NotEmpty(t, state)
	require.NotEmpty(t, capturedChallenge)

	res, err := h.svc.Exchange(ctx, "okta-prod", "oidc-code-1", state, "https://opspilot.test/login/sso/callback?provider=okta-prod")
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Equal(t, "alice@corp.example.com", res.User.Email)
	require.Equal(t, "/devices", res.RedirectTo)

	// 验证自动加入默认组织
	ms, err := h.msSvc.ListByUser(ctx, res.User.ID)
	require.NoError(t, err)
	require.Len(t, ms, 1)

	// 重放同一 state 必须失败
	_, err = h.svc.Exchange(ctx, "okta-prod", "oidc-code-1", state, "https://opspilot.test/login/sso/callback?provider=okta-prod")
	require.ErrorIs(t, err, errs.ErrUnauthorized)
}

func TestFeishuAndWeComAndDomainWhitelist(t *testing.T) {
	h := newSSOTestHarness(t)
	ctx := context.Background()

	var currentEmail = "bob@evil.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feishu/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         0,
				"access_token": "fs-access-1",
			})
		case "/feishu/userinfo":
			require.Equal(t, "Bearer fs-access-1", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"union_id":         "on_feishu_u1",
					"enterprise_email": currentEmail,
					"name":             "Bob Feishu",
				},
			})
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":      0,
				"access_token": "wc-app-tok",
			})
		case "/cgi-bin/auth/getuserinfo":
			require.Equal(t, "wc-code-1", r.URL.Query().Get("code"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0,
				"userid":  "chenwei",
			})
		case "/cgi-bin/user/get":
			require.Equal(t, "chenwei", r.URL.Query().Get("userid"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode":  0,
				"userid":   "chenwei",
				"name":     "Chen Wei",
				"biz_mail": "chenwei@corp.example.com",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	_, err := h.svc.CreateProvider(ctx, ProviderInput{
		Slug:           "feishu-corp",
		Name:           "飞书企业",
		Type:           model.SSOTypeFeishu,
		Enabled:        true,
		AuthURL:        srv.URL + "/feishu/auth",
		TokenURL:       srv.URL + "/feishu/token",
		UserInfoURL:    srv.URL + "/feishu/userinfo",
		ClientID:       "cli_fs",
		ClientSecret:   "sec_fs",
		AutoCreateUser: true,
		DefaultRole:    model.RoleUser,
		AllowedDomains: "corp.example.com",
	})
	require.NoError(t, err)

	// 域外邮箱被 AllowedDomains 拒绝
	authURL, err := h.svc.StartLogin(ctx, "feishu-corp", "https://opspilot.test/cb", "/")
	require.NoError(t, err)
	u1, _ := url.Parse(authURL)
	_, err = h.svc.Exchange(ctx, "feishu-corp", "fs-code", u1.Query().Get("state"), "https://opspilot.test/cb")
	require.ErrorIs(t, err, errs.ErrForbidden)

	// 换为允许域名后登录成功
	currentEmail = "bob@corp.example.com"
	authURL2, err := h.svc.StartLogin(ctx, "feishu-corp", "https://opspilot.test/cb", "/")
	require.NoError(t, err)
	u2, _ := url.Parse(authURL2)
	resFS, err := h.svc.Exchange(ctx, "feishu-corp", "fs-code", u2.Query().Get("state"), "https://opspilot.test/cb")
	require.NoError(t, err)
	require.Equal(t, "bob@corp.example.com", resFS.User.Email)

	// 测试企业微信扫码登录
	_, err = h.svc.CreateProvider(ctx, ProviderInput{
		Slug:           "wecom-corp",
		Name:           "企业微信",
		Type:           model.SSOTypeWeCom,
		Enabled:        true,
		AuthURL:        srv.URL + "/wwlogin/sso/login",
		TokenURL:       srv.URL + "/cgi-bin/gettoken",
		CorpID:         "ww12345",
		AgentID:        "1000002",
		ClientSecret:   "sec_wc",
		AutoCreateUser: true,
		DefaultRole:    model.RoleUser,
	})
	require.NoError(t, err)

	wcURL, err := h.svc.StartLogin(ctx, "wecom-corp", "https://opspilot.test/cb", "/")
	require.NoError(t, err)
	require.True(t, strings.Contains(wcURL, "appid=ww12345"))
	u3, _ := url.Parse(wcURL)
	resWC, err := h.svc.Exchange(ctx, "wecom-corp", "wc-code-1", u3.Query().Get("state"), "https://opspilot.test/cb")
	require.NoError(t, err)
	require.Equal(t, "chenwei@corp.example.com", resWC.User.Email)
}
