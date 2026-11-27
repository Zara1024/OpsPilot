// Package sso 实现企业级单点登录（标准 OIDC / 飞书 / 企业微信），包含 provider 管理、一次性 state/PKCE、JIT 用户开通与身份绑定。
package sso

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/secretbox"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,62}$`)

// Store 定义 SSO 持久化依赖。
type Store interface {
	ListSSOProviders(ctx context.Context, onlyEnabled bool) ([]*model.SSOProvider, error)
	GetSSOProviderByID(ctx context.Context, id uint64) (*model.SSOProvider, error)
	GetSSOProviderBySlug(ctx context.Context, slug string) (*model.SSOProvider, error)
	CreateSSOProvider(ctx context.Context, p *model.SSOProvider) error
	UpdateSSOProvider(ctx context.Context, p *model.SSOProvider) error
	DeleteSSOProvider(ctx context.Context, id uint64) error
	CreateSSOState(ctx context.Context, st *model.SSOState) error
	ConsumeSSOState(ctx context.Context, state string, now time.Time) (*model.SSOState, error)
	GetSSOIdentity(ctx context.Context, providerID uint64, externalSub string) (*model.SSOIdentity, error)
	UpsertSSOIdentity(ctx context.Context, ident *model.SSOIdentity) error
	ListSSOIdentitiesByUser(ctx context.Context, userID uint64) ([]model.SSOIdentityWithProvider, error)
	DeleteSSOIdentity(ctx context.Context, userID, identityID uint64) error
}

// UserManager 定义 SSO 登录关联/自动建号所需的用户接口。
type UserManager interface {
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	EnsureSSOUser(ctx context.Context, email, displayName, defaultRole string, autoCreate bool) (*model.User, bool, error)
}

// OrgJoiner 定义新开通 SSO 用户的默认组织加入接口。
type OrgJoiner interface {
	AddOrUpdate(ctx context.Context, userID, orgID uint64, role string) (*model.OrgMembership, error)
}

// DefaultOrgResolver 查找平台默认组织 ID。
type DefaultOrgResolver interface {
	List(ctx context.Context) ([]*model.Org, error)
}

// PublicProvider 描述登录页公开可见的 SSO 入口摘要（绝不含任何密钥与内网凭据）。
type PublicProvider struct {
	ID   uint64 `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ProviderInput 为管理员创建/更新 SSO 提供商的入参。
type ProviderInput struct {
	Slug           string  `json:"slug"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Enabled        bool    `json:"enabled"`
	IssuerURL      string  `json:"issuer_url"`
	AuthURL        string  `json:"auth_url"`
	TokenURL       string  `json:"token_url"`
	UserInfoURL    string  `json:"userinfo_url"`
	JWKSURL        string  `json:"jwks_url"`
	ClientID       string  `json:"client_id"`
	ClientSecret   string  `json:"client_secret"`
	CorpID         string  `json:"corp_id"`
	AgentID        string  `json:"agent_id"`
	Scopes         string  `json:"scopes"`
	AutoCreateUser bool    `json:"auto_create_user"`
	DefaultRole    string  `json:"default_role"`
	DefaultOrgID   *uint64 `json:"default_org_id"`
	AllowedDomains string  `json:"allowed_domains"`
}

// ExchangeResult 返回完成 SSO 回调后匹配/创建的平台用户与外部身份摘要。
type ExchangeResult struct {
	User         *model.User
	Created      bool
	ProviderSlug string
	RedirectTo   string
	ExternalSub  string
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	expiresAt             time.Time
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type cachedJWKS struct {
	keys      []jwkKey
	expiresAt time.Time
}

// Service 编排 SSO 配置与登录换码流程。
type Service struct {
	store      Store
	users      UserManager
	orgJoiner  OrgJoiner
	orgFinder  DefaultOrgResolver
	httpClient *http.Client
	now        func() time.Time

	mu             sync.RWMutex
	discoveryCache map[string]oidcDiscovery
	jwksCache      map[string]cachedJWKS
}

// New 构造 SSO Service。
func New(store Store, users UserManager) *Service {
	return &Service{
		store:          store,
		users:          users,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		now:            func() time.Time { return time.Now().UTC() },
		discoveryCache: map[string]oidcDiscovery{},
		jwksCache:      map[string]cachedJWKS{},
	}
}

// SetOrgWiring 注入组织成员服务与组织列表解析器，使 SSO 自动建号的用户获得组织成员身份。
func (s *Service) SetOrgWiring(joiner OrgJoiner, finder DefaultOrgResolver) {
	s.orgJoiner = joiner
	s.orgFinder = finder
}

// ListPublic 返回已启用的公开 SSO 登录入口列表。
func (s *Service) ListPublic(ctx context.Context) ([]PublicProvider, error) {
	rows, err := s.store.ListSSOProviders(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]PublicProvider, 0, len(rows))
	for _, r := range rows {
		out = append(out, PublicProvider{
			ID:   r.ID,
			Slug: r.Slug,
			Name: r.Name,
			Type: r.Type,
		})
	}
	return out, nil
}

// ListAll 列出全部 SSO 提供商配置（管理员视图）。
func (s *Service) ListAll(ctx context.Context) ([]*model.SSOProvider, error) {
	return s.store.ListSSOProviders(ctx, false)
}

// CreateProvider 校验并创建新的 SSO 提供商。
func (s *Service) CreateProvider(ctx context.Context, in ProviderInput) (*model.SSOProvider, error) {
	p := &model.SSOProvider{}
	if err := s.applyInput(p, in, true); err != nil {
		return nil, err
	}
	if existing, err := s.store.GetSSOProviderBySlug(ctx, p.Slug); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: sso slug %q already exists", errs.ErrConflict, p.Slug)
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}
	if err := s.store.CreateSSOProvider(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateProvider 更新 SSO 提供商；当 ClientSecret 留空时保留原有加密密钥。
func (s *Service) UpdateProvider(ctx context.Context, id uint64, in ProviderInput) (*model.SSOProvider, error) {
	p, err := s.store.GetSSOProviderByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.applyInput(p, in, false); err != nil {
		return nil, err
	}
	if other, err := s.store.GetSSOProviderBySlug(ctx, p.Slug); err == nil && other != nil && other.ID != id {
		return nil, fmt.Errorf("%w: sso slug %q already exists", errs.ErrConflict, p.Slug)
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	}
	if err := s.store.UpdateSSOProvider(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteProvider 删除 SSO 提供商及其关联绑定。
func (s *Service) DeleteProvider(ctx context.Context, id uint64) error {
	return s.store.DeleteSSOProvider(ctx, id)
}

// ListUserIdentities 列出用户已关联的外部 SSO 账号。
func (s *Service) ListUserIdentities(ctx context.Context, userID uint64) ([]model.SSOIdentityWithProvider, error) {
	return s.store.ListSSOIdentitiesByUser(ctx, userID)
}

// UnlinkUserIdentity 解绑用户的一条外部 SSO 身份。
func (s *Service) UnlinkUserIdentity(ctx context.Context, userID, identityID uint64) error {
	return s.store.DeleteSSOIdentity(ctx, userID, identityID)
}

func (s *Service) applyInput(dst *model.SSOProvider, in ProviderInput, isCreate bool) error {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: slug must match ^[a-z0-9][a-z0-9_-]{1,62}$", errs.ErrInvalid)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 128 {
		return fmt.Errorf("%w: name required (max 128 chars)", errs.ErrInvalid)
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if !model.IsValidSSOType(typ) {
		return fmt.Errorf("%w: unsupported sso type %q", errs.ErrInvalid, in.Type)
	}
	defaultRole := strings.TrimSpace(in.DefaultRole)
	if defaultRole == "" {
		defaultRole = model.RoleUser
	}
	if !model.IsValidRole(defaultRole) {
		return fmt.Errorf("%w: invalid default_role %q", errs.ErrInvalid, defaultRole)
	}

	clientID := strings.TrimSpace(in.ClientID)
	corpID := strings.TrimSpace(in.CorpID)
	if typ == model.SSOTypeWeCom {
		if corpID == "" && clientID == "" {
			return fmt.Errorf("%w: corp_id required for WeCom provider", errs.ErrInvalid)
		}
		if corpID == "" {
			corpID = clientID
		}
		if clientID == "" {
			clientID = corpID
		}
		if strings.TrimSpace(in.AgentID) == "" {
			return fmt.Errorf("%w: agent_id required for WeCom provider", errs.ErrInvalid)
		}
	} else {
		if clientID == "" {
			return fmt.Errorf("%w: client_id required", errs.ErrInvalid)
		}
		if typ == model.SSOTypeOIDC && strings.TrimSpace(in.IssuerURL) == "" &&
			(strings.TrimSpace(in.AuthURL) == "" || strings.TrimSpace(in.TokenURL) == "") {
			return fmt.Errorf("%w: issuer_url or explicit auth_url/token_url required for OIDC", errs.ErrInvalid)
		}
	}

	if secret := strings.TrimSpace(in.ClientSecret); secret != "" {
		enc, err := secretbox.Encrypt(secret)
		if err != nil {
			return fmt.Errorf("encrypt client_secret: %w", err)
		}
		dst.ClientSecretEnc = enc
	} else if isCreate && dst.ClientSecretEnc == "" {
		// OIDC public client with PKCE 理论上可不配密钥，仅校验非空常见场景时仍允许飞书/企微校验：
		if typ == model.SSOTypeFeishu || typ == model.SSOTypeWeCom {
			return fmt.Errorf("%w: client_secret required for %s provider", errs.ErrInvalid, typ)
		}
	}

	dst.Slug = slug
	dst.Name = name
	dst.Type = typ
	dst.Enabled = in.Enabled
	dst.IssuerURL = strings.TrimSpace(in.IssuerURL)
	dst.AuthURL = strings.TrimSpace(in.AuthURL)
	dst.TokenURL = strings.TrimSpace(in.TokenURL)
	dst.UserInfoURL = strings.TrimSpace(in.UserInfoURL)
	dst.JWKSURL = strings.TrimSpace(in.JWKSURL)
	dst.ClientID = clientID
	dst.CorpID = corpID
	dst.AgentID = strings.TrimSpace(in.AgentID)
	dst.Scopes = strings.TrimSpace(in.Scopes)
	dst.AutoCreateUser = in.AutoCreateUser
	dst.DefaultRole = defaultRole
	dst.DefaultOrgID = in.DefaultOrgID
	dst.AllowedDomains = normalizeDomains(in.AllowedDomains)
	return nil
}

// StartLogin 生成一次性 state / PKCE verifier 并构造第三方 IdP 登录跳转地址。
func (s *Service) StartLogin(ctx context.Context, slug, callbackURL, redirectTo string) (string, error) {
	prov, err := s.store.GetSSOProviderBySlug(ctx, slug)
	if err != nil {
		return "", err
	}
	if !prov.Enabled {
		return "", fmt.Errorf("%w: SSO provider is disabled", errs.ErrForbidden)
	}
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return "", fmt.Errorf("%w: callback URL required", errs.ErrInvalid)
	}

	stateVal, err := randomURLSafe(24)
	if err != nil {
		return "", err
	}
	verifier, err := randomURLSafe(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomURLSafe(24)
	if err != nil {
		return "", err
	}

	safeRedirect := sanitizeRedirect(redirectTo)
	if err := s.store.CreateSSOState(ctx, &model.SSOState{
		State:        stateVal,
		ProviderSlug: prov.Slug,
		CodeVerifier: verifier,
		Nonce:        nonce,
		RedirectTo:   safeRedirect,
		ExpiresAt:    s.now().Add(10 * time.Minute),
	}); err != nil {
		return "", err
	}

	switch prov.Type {
	case model.SSOTypeOIDC:
		disc, err := s.resolveOIDCConfig(ctx, prov)
		if err != nil {
			return "", err
		}
		if disc.AuthorizationEndpoint == "" {
			return "", fmt.Errorf("%w: OIDC authorization_endpoint not configured", errs.ErrInvalid)
		}
		u, err := url.Parse(disc.AuthorizationEndpoint)
		if err != nil {
			return "", fmt.Errorf("%w: invalid auth_url: %v", errs.ErrInvalid, err)
		}
		scopes := strings.TrimSpace(prov.Scopes)
		if scopes == "" {
			scopes = "openid email profile"
		}
		q := u.Query()
		q.Set("response_type", "code")
		q.Set("client_id", prov.ClientID)
		q.Set("redirect_uri", callbackURL)
		q.Set("scope", scopes)
		q.Set("state", stateVal)
		q.Set("nonce", nonce)
		q.Set("code_challenge", pkceS256(verifier))
		q.Set("code_challenge_method", "S256")
		u.RawQuery = q.Encode()
		return u.String(), nil

	case model.SSOTypeFeishu:
		authBase := strings.TrimSpace(prov.AuthURL)
		if authBase == "" {
			authBase = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
		}
		u, err := url.Parse(authBase)
		if err != nil {
			return "", fmt.Errorf("%w: invalid feishu auth_url: %v", errs.ErrInvalid, err)
		}
		q := u.Query()
		q.Set("client_id", prov.ClientID)
		q.Set("redirect_uri", callbackURL)
		q.Set("response_type", "code")
		q.Set("state", stateVal)
		if strings.TrimSpace(prov.Scopes) != "" {
			q.Set("scope", strings.TrimSpace(prov.Scopes))
		}
		u.RawQuery = q.Encode()
		return u.String(), nil

	case model.SSOTypeWeCom:
		authBase := strings.TrimSpace(prov.AuthURL)
		if authBase == "" {
			authBase = "https://login.work.weixin.qq.com/wwlogin/sso/login"
		}
		u, err := url.Parse(authBase)
		if err != nil {
			return "", fmt.Errorf("%w: invalid wecom auth_url: %v", errs.ErrInvalid, err)
		}
		appID := prov.CorpID
		if appID == "" {
			appID = prov.ClientID
		}
		q := u.Query()
		q.Set("login_type", "CorpApp")
		q.Set("appid", appID)
		q.Set("agentid", prov.AgentID)
		q.Set("redirect_uri", callbackURL)
		q.Set("state", stateVal)
		u.RawQuery = q.Encode()
		return u.String(), nil

	default:
		return "", fmt.Errorf("%w: unsupported provider type %q", errs.ErrInvalid, prov.Type)
	}
}

// Exchange 使用回调返回的 code + state 向 IdP 换取身份信息、校验域名白名单并完成用户匹配/JIT 开通。
func (s *Service) Exchange(ctx context.Context, slug, code, state, callbackURL string) (*ExchangeResult, error) {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return nil, fmt.Errorf("%w: code and state required", errs.ErrInvalid)
	}
	st, err := s.store.ConsumeSSOState(ctx, state, s.now())
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, fmt.Errorf("%w: invalid or already used SSO state", errs.ErrUnauthorized)
		}
		return nil, err
	}
	if st.ProviderSlug != strings.ToLower(strings.TrimSpace(slug)) {
		return nil, fmt.Errorf("%w: SSO provider state mismatch", errs.ErrUnauthorized)
	}

	prov, err := s.store.GetSSOProviderBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !prov.Enabled {
		return nil, fmt.Errorf("%w: SSO provider is disabled", errs.ErrForbidden)
	}
	secret, err := secretbox.Decrypt(prov.ClientSecretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt sso client secret: %w", err)
	}

	var ext externalIdentity
	switch prov.Type {
	case model.SSOTypeOIDC:
		ext, err = s.exchangeOIDC(ctx, prov, secret, code, callbackURL, st.CodeVerifier, st.Nonce)
	case model.SSOTypeFeishu:
		ext, err = s.exchangeFeishu(ctx, prov, secret, code, callbackURL)
	case model.SSOTypeWeCom:
		ext, err = s.exchangeWeCom(ctx, prov, secret, code)
	default:
		return nil, fmt.Errorf("%w: unsupported provider type %q", errs.ErrInvalid, prov.Type)
	}
	if err != nil {
		return nil, err
	}

	ext.Email = strings.ToLower(strings.TrimSpace(ext.Email))
	if ext.Sub == "" {
		return nil, fmt.Errorf("%w: SSO identity missing sub", errs.ErrUnauthorized)
	}
	if !isDomainAllowed(ext.Email, prov.AllowedDomains) {
		return nil, fmt.Errorf("%w: email domain is not allowed for this SSO provider", errs.ErrForbidden)
	}

	now := s.now()
	var (
		user    *model.User
		created bool
	)
	if existingIdent, err := s.store.GetSSOIdentity(ctx, prov.ID, ext.Sub); err == nil && existingIdent != nil {
		user, err = s.users.GetByID(ctx, existingIdent.UserID)
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				// 原平台用户已删，按 email 重连或重新创建
				user, created, err = s.users.EnsureSSOUser(ctx, ext.Email, ext.Name, prov.DefaultRole, prov.AutoCreateUser)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else if user.Status != model.StatusActive {
			return nil, fmt.Errorf("%w: user account is disabled", errs.ErrForbidden)
		}
	} else if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return nil, err
	} else {
		user, created, err = s.users.EnsureSSOUser(ctx, ext.Email, ext.Name, prov.DefaultRole, prov.AutoCreateUser)
		if err != nil {
			return nil, err
		}
	}

	if err := s.store.UpsertSSOIdentity(ctx, &model.SSOIdentity{
		ProviderID:    prov.ID,
		ExternalSub:   ext.Sub,
		UserID:        user.ID,
		ExternalEmail: ext.Email,
		ExternalName:  ext.Name,
		LastLoginAt:   &now,
	}); err != nil {
		return nil, fmt.Errorf("persist sso identity: %w", err)
	}

	if created {
		s.ensureDefaultOrgMembership(ctx, prov, user)
	}

	return &ExchangeResult{
		User:         user,
		Created:      created,
		ProviderSlug: prov.Slug,
		RedirectTo:   st.RedirectTo,
		ExternalSub:  ext.Sub,
	}, nil
}

func (s *Service) ensureDefaultOrgMembership(ctx context.Context, prov *model.SSOProvider, user *model.User) {
	if s.orgJoiner == nil || user == nil {
		return
	}
	var targetOrgID uint64
	if prov.DefaultOrgID != nil && *prov.DefaultOrgID > 0 {
		targetOrgID = *prov.DefaultOrgID
	} else if s.orgFinder != nil {
		if orgs, err := s.orgFinder.List(ctx); err == nil {
			for _, o := range orgs {
				if o != nil && o.ParentID == nil {
					targetOrgID = o.ID
					break
				}
			}
			if targetOrgID == 0 && len(orgs) > 0 && orgs[0] != nil {
				targetOrgID = orgs[0].ID
			}
		}
	}
	if targetOrgID == 0 {
		return
	}
	mRole := model.MembershipRoleMember
	if user.Role == model.RoleAdmin {
		mRole = model.MembershipRoleAdmin
	} else if user.Role == model.RoleViewer {
		mRole = model.MembershipRoleViewer
	}
	// 默认组织成员补齐属于最佳努力增强，失败不阻断本次 SSO 登录返回。
	_, _ = s.orgJoiner.AddOrUpdate(ctx, user.ID, targetOrgID, mRole)
}

type externalIdentity struct {
	Sub   string
	Email string
	Name  string
}

func (s *Service) resolveOIDCConfig(ctx context.Context, prov *model.SSOProvider) (oidcDiscovery, error) {
	out := oidcDiscovery{
		Issuer:                strings.TrimSpace(prov.IssuerURL),
		AuthorizationEndpoint: strings.TrimSpace(prov.AuthURL),
		TokenEndpoint:         strings.TrimSpace(prov.TokenURL),
		UserinfoEndpoint:      strings.TrimSpace(prov.UserInfoURL),
		JWKSURI:               strings.TrimSpace(prov.JWKSURL),
	}
	if out.Issuer == "" || (out.AuthorizationEndpoint != "" && out.TokenEndpoint != "" && (out.JWKSURI != "" || out.UserinfoEndpoint != "")) {
		return out, nil
	}

	issuerBase := strings.TrimRight(out.Issuer, "/")
	s.mu.RLock()
	cached, ok := s.discoveryCache[issuerBase]
	s.mu.RUnlock()
	if !ok || s.now().After(cached.expiresAt) {
		discURL := issuerBase + "/.well-known/openid-configuration"
		var fetched oidcDiscovery
		if err := s.getJSON(ctx, discURL, "", &fetched); err != nil {
			return oidcDiscovery{}, fmt.Errorf("fetch oidc discovery %s: %w", discURL, err)
		}
		fetched.expiresAt = s.now().Add(5 * time.Minute)
		s.mu.Lock()
		s.discoveryCache[issuerBase] = fetched
		s.mu.Unlock()
		cached = fetched
	}

	if out.AuthorizationEndpoint == "" {
		out.AuthorizationEndpoint = cached.AuthorizationEndpoint
	}
	if out.TokenEndpoint == "" {
		out.TokenEndpoint = cached.TokenEndpoint
	}
	if out.UserinfoEndpoint == "" {
		out.UserinfoEndpoint = cached.UserinfoEndpoint
	}
	if out.JWKSURI == "" {
		out.JWKSURI = cached.JWKSURI
	}
	return out, nil
}

func (s *Service) exchangeOIDC(ctx context.Context, prov *model.SSOProvider, clientSecret, code, callbackURL, codeVerifier, expectedNonce string) (externalIdentity, error) {
	disc, err := s.resolveOIDCConfig(ctx, prov)
	if err != nil {
		return externalIdentity{}, err
	}
	if disc.TokenEndpoint == "" {
		return externalIdentity{}, fmt.Errorf("%w: OIDC token_endpoint not configured", errs.ErrInvalid)
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", callbackURL)
	form.Set("client_id", prov.ClientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return externalIdentity{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := s.doJSON(req, &tokenResp); err != nil {
		return externalIdentity{}, fmt.Errorf("oidc token exchange: %w", err)
	}
	if tokenResp.Error != "" {
		return externalIdentity{}, fmt.Errorf("%w: oidc error %s (%s)", errs.ErrUnauthorized, tokenResp.Error, tokenResp.ErrorDesc)
	}

	var ext externalIdentity
	if tokenResp.IDToken != "" {
		if disc.JWKSURI == "" {
			return externalIdentity{}, fmt.Errorf("%w: jwks_uri required to verify id_token", errs.ErrInvalid)
		}
		claims, err := s.verifyIDToken(ctx, tokenResp.IDToken, disc.JWKSURI, disc.Issuer, prov.ClientID, expectedNonce)
		if err != nil {
			return externalIdentity{}, err
		}
		ext.Sub = stringClaim(claims, "sub")
		ext.Email = stringClaim(claims, "email")
		ext.Name = firstNonEmpty(stringClaim(claims, "name"), stringClaim(claims, "preferred_username"))
	}

	if (ext.Sub == "" || ext.Email == "") && tokenResp.AccessToken != "" && disc.UserinfoEndpoint != "" {
		var ui map[string]any
		if err := s.getJSON(ctx, disc.UserinfoEndpoint, tokenResp.AccessToken, &ui); err == nil {
			if ext.Sub == "" {
				ext.Sub = stringClaim(ui, "sub")
			}
			if ext.Email == "" {
				ext.Email = stringClaim(ui, "email")
			}
			if ext.Name == "" {
				ext.Name = firstNonEmpty(stringClaim(ui, "name"), stringClaim(ui, "preferred_username"))
			}
		}
	}
	return ext, nil
}

func (s *Service) verifyIDToken(ctx context.Context, rawToken, jwksURI, expectedIssuer, expectedAud, expectedNonce string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(rawToken, claims, func(tok *jwt.Token) (any, error) {
		switch tok.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS, *jwt.SigningMethodECDSA:
		default:
			return nil, fmt.Errorf("unexpected id_token alg: %v", tok.Header["alg"])
		}
		kid, _ := tok.Header["kid"].(string)
		return s.lookupJWKPublicKey(ctx, jwksURI, kid)
	}, jwt.WithTimeFunc(s.now))
	if err != nil || !parsed.Valid {
		return nil, fmt.Errorf("%w: invalid id_token: %v", errs.ErrUnauthorized, err)
	}
	if expectedIssuer != "" {
		if iss := strings.TrimRight(stringClaim(claims, "iss"), "/"); iss != strings.TrimRight(expectedIssuer, "/") {
			return nil, fmt.Errorf("%w: id_token issuer mismatch", errs.ErrUnauthorized)
		}
	}
	if expectedAud != "" {
		auds, _ := claims.GetAudience()
		matched := false
		for _, a := range auds {
			if a == expectedAud {
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("%w: id_token audience mismatch", errs.ErrUnauthorized)
		}
	}
	if expectedNonce != "" {
		if n := stringClaim(claims, "nonce"); n != "" && n != expectedNonce {
			return nil, fmt.Errorf("%w: id_token nonce mismatch", errs.ErrUnauthorized)
		}
	}
	return claims, nil
}

func (s *Service) lookupJWKPublicKey(ctx context.Context, jwksURI, kid string) (any, error) {
	keys, err := s.fetchJWKS(ctx, jwksURI, false)
	if err != nil {
		return nil, err
	}
	if key := findJWK(keys, kid); key != nil {
		return parseJWK(key)
	}
	// kid 未命中时强制刷新一次 JWKS 以应对密钥轮换。
	keys, err = s.fetchJWKS(ctx, jwksURI, true)
	if err != nil {
		return nil, err
	}
	if key := findJWK(keys, kid); key != nil {
		return parseJWK(key)
	}
	return nil, fmt.Errorf("no matching JWK found for kid=%q", kid)
}

func (s *Service) fetchJWKS(ctx context.Context, jwksURI string, force bool) ([]jwkKey, error) {
	if !force {
		s.mu.RLock()
		c, ok := s.jwksCache[jwksURI]
		s.mu.RUnlock()
		if ok && s.now().Before(c.expiresAt) {
			return c.keys, nil
		}
	}
	var payload struct {
		Keys []jwkKey `json:"keys"`
	}
	if err := s.getJSON(ctx, jwksURI, "", &payload); err != nil {
		return nil, fmt.Errorf("fetch jwks %s: %w", jwksURI, err)
	}
	s.mu.Lock()
	s.jwksCache[jwksURI] = cachedJWKS{keys: payload.Keys, expiresAt: s.now().Add(5 * time.Minute)}
	s.mu.Unlock()
	return payload.Keys, nil
}

func findJWK(keys []jwkKey, kid string) *jwkKey {
	for i := range keys {
		k := &keys[i]
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if kid == "" || k.Kid == kid {
			return k
		}
	}
	return nil
}

func parseJWK(k *jwkKey) (any, error) {
	switch k.Kty {
	case "RSA":
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("decode jwk n: %w", err)
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("decode jwk e: %w", err)
		}
		eVal := int(new(big.Int).SetBytes(eb).Int64())
		if eVal <= 0 {
			return nil, errors.New("invalid jwk rsa exponent")
		}
		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nb),
			E: eVal,
		}, nil
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported EC curve %q", k.Crv)
		}
		xb, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, fmt.Errorf("decode jwk x: %w", err)
		}
		yb, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, fmt.Errorf("decode jwk y: %w", err)
		}
		return &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xb),
			Y:     new(big.Int).SetBytes(yb),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported jwk kty %q", k.Kty)
	}
}

func (s *Service) exchangeFeishu(ctx context.Context, prov *model.SSOProvider, clientSecret, code, callbackURL string) (externalIdentity, error) {
	tokenEndpoint := strings.TrimSpace(prov.TokenURL)
	if tokenEndpoint == "" {
		tokenEndpoint = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	}
	bodyBytes, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     prov.ClientID,
		"client_secret": clientSecret,
		"code":          code,
		"redirect_uri":  callbackURL,
	})
	if err != nil {
		return externalIdentity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return externalIdentity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var tokenResp struct {
		Code        int    `json:"code"`
		Msg         string `json:"msg"`
		Message     string `json:"message"`
		AccessToken string `json:"access_token"`
		Data        struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := s.doJSON(req, &tokenResp); err != nil {
		return externalIdentity{}, fmt.Errorf("feishu token exchange: %w", err)
	}
	if tokenResp.Code != 0 {
		return externalIdentity{}, fmt.Errorf("%w: feishu token error (%d): %s", errs.ErrUnauthorized, tokenResp.Code, firstNonEmpty(tokenResp.Msg, tokenResp.Message))
	}
	accessToken := firstNonEmpty(tokenResp.AccessToken, tokenResp.Data.AccessToken)
	if accessToken == "" {
		return externalIdentity{}, fmt.Errorf("%w: feishu empty access_token", errs.ErrUnauthorized)
	}

	userInfoEndpoint := strings.TrimSpace(prov.UserInfoURL)
	if userInfoEndpoint == "" {
		userInfoEndpoint = "https://open.feishu.cn/open-apis/authen/v1/user_info"
	}
	var uiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Sub             string `json:"sub"`
			OpenID          string `json:"open_id"`
			UnionID         string `json:"union_id"`
			Name            string `json:"name"`
			Email           string `json:"email"`
			EnterpriseEmail string `json:"enterprise_email"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, userInfoEndpoint, accessToken, &uiResp); err != nil {
		return externalIdentity{}, fmt.Errorf("feishu user_info: %w", err)
	}
	if uiResp.Code != 0 {
		return externalIdentity{}, fmt.Errorf("%w: feishu user_info error (%d): %s", errs.ErrUnauthorized, uiResp.Code, uiResp.Msg)
	}
	return externalIdentity{
		Sub:   firstNonEmpty(uiResp.Data.UnionID, uiResp.Data.OpenID, uiResp.Data.Sub),
		Email: firstNonEmpty(uiResp.Data.EnterpriseEmail, uiResp.Data.Email),
		Name:  uiResp.Data.Name,
	}, nil
}

func (s *Service) exchangeWeCom(ctx context.Context, prov *model.SSOProvider, clientSecret, code string) (externalIdentity, error) {
	apiBase := "https://qyapi.weixin.qq.com"
	tokenURL := strings.TrimSpace(prov.TokenURL)
	if tokenURL != "" {
		if parsed, err := url.Parse(tokenURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			apiBase = parsed.Scheme + "://" + parsed.Host
		}
	}
	corpID := firstNonEmpty(prov.CorpID, prov.ClientID)
	getTokenEndpoint := tokenURL
	if getTokenEndpoint == "" {
		getTokenEndpoint = fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", apiBase, url.QueryEscape(corpID), url.QueryEscape(clientSecret))
	}
	var tokResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	if err := s.getJSON(ctx, getTokenEndpoint, "", &tokResp); err != nil {
		return externalIdentity{}, fmt.Errorf("wecom gettoken: %w", err)
	}
	if tokResp.ErrCode != 0 || tokResp.AccessToken == "" {
		return externalIdentity{}, fmt.Errorf("%w: wecom gettoken error (%d): %s", errs.ErrUnauthorized, tokResp.ErrCode, tokResp.ErrMsg)
	}

	userInfoURL := fmt.Sprintf("%s/cgi-bin/auth/getuserinfo?access_token=%s&code=%s", apiBase, url.QueryEscape(tokResp.AccessToken), url.QueryEscape(code))
	if custom := strings.TrimSpace(prov.UserInfoURL); custom != "" && strings.Contains(custom, "getuserinfo") {
		userInfoURL = custom + "?access_token=" + url.QueryEscape(tokResp.AccessToken) + "&code=" + url.QueryEscape(code)
	}
	var infoResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		UserID  string `json:"userid"`
		OpenID  string `json:"openid"`
	}
	if err := s.getJSON(ctx, userInfoURL, "", &infoResp); err != nil {
		return externalIdentity{}, fmt.Errorf("wecom getuserinfo: %w", err)
	}
	if infoResp.ErrCode != 0 {
		return externalIdentity{}, fmt.Errorf("%w: wecom getuserinfo error (%d): %s", errs.ErrUnauthorized, infoResp.ErrCode, infoResp.ErrMsg)
	}
	if infoResp.UserID == "" {
		return externalIdentity{}, fmt.Errorf("%w: wecom user is outside the enterprise (missing userid)", errs.ErrForbidden)
	}

	detailURL := fmt.Sprintf("%s/cgi-bin/user/get?access_token=%s&userid=%s", apiBase, url.QueryEscape(tokResp.AccessToken), url.QueryEscape(infoResp.UserID))
	var detailResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		UserID  string `json:"userid"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		BizMail string `json:"biz_mail"`
	}
	if err := s.getJSON(ctx, detailURL, "", &detailResp); err != nil {
		return externalIdentity{}, fmt.Errorf("wecom user get: %w", err)
	}
	if detailResp.ErrCode != 0 {
		return externalIdentity{}, fmt.Errorf("%w: wecom user get error (%d): %s", errs.ErrUnauthorized, detailResp.ErrCode, detailResp.ErrMsg)
	}
	return externalIdentity{
		Sub:   firstNonEmpty(detailResp.UserID, infoResp.UserID),
		Email: firstNonEmpty(detailResp.BizMail, detailResp.Email),
		Name:  detailResp.Name,
	}, nil
}

func (s *Service) getJSON(ctx context.Context, targetURL, bearer string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return s.doJSON(req, dst)
}

func (s *Service) doJSON(req *http.Request, dst any) error {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // 关闭响应体以复用连接
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, dst)
}

func randomURLSafe(nBytes int) (string, error) {
	raw := make([]byte, nBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func sanitizeRedirect(r string) string {
	r = strings.TrimSpace(r)
	if strings.HasPrefix(r, "/") && !strings.HasPrefix(r, "//") {
		return r
	}
	return "/"
}

func normalizeDomains(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' '
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p), "@"))
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, ",")
}

func isDomainAllowed(email, allowedCSV string) bool {
	if strings.TrimSpace(allowedCSV) == "" {
		return true
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, allowed := range strings.Split(allowedCSV, ",") {
		if strings.TrimSpace(strings.ToLower(allowed)) == domain {
			return true
		}
	}
	return false
}

func stringClaim(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
