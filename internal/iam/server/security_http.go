package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Zara1024/OpsPilot/internal/iam/biz/sso"
	"github.com/Zara1024/OpsPilot/internal/iam/biz/throttle"
	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/auditctx"
	"github.com/Zara1024/OpsPilot/internal/pkg/auth"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
	"github.com/Zara1024/OpsPilot/internal/pkg/tenantctx"
)

func (h *Handler) checkLoginThrottle(r *http.Request, ip, target string) error {
	if tSvc := h.svc.Throttle(); tSvc != nil {
		return tSvc.Check(r.Context(), ip, target)
	}
	return h.throttle.check(ip, target)
}

func (h *Handler) recordLoginFailure(r *http.Request, ip, target string) {
	if tSvc := h.svc.Throttle(); tSvc != nil {
		if err := tSvc.RecordFailure(r.Context(), ip, target); err != nil && h.log != nil {
			h.log.Warn("iam: record login failure failed", "err", err)
		}
		return
	}
	h.throttle.recordFailure(ip, target)
}

func (h *Handler) recordLoginSuccess(r *http.Request, target string) {
	if tSvc := h.svc.Throttle(); tSvc != nil {
		if err := tSvc.RecordSuccess(r.Context(), target); err != nil && h.log != nil {
			h.log.Warn("iam: record login success failed", "err", err)
		}
		return
	}
	h.throttle.recordSuccess(target)
}

func (h *Handler) completeAuthenticatedLogin(w http.ResponseWriter, r *http.Request, u *model.User, redirectTo string) {
	if mfaSvc := h.svc.MFA(); mfaSvc != nil {
		enabled, err := mfaSvc.IsEnabled(r.Context(), u.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if enabled {
			tok, err := h.svc.User().IssueMFAToken(u, false, 5*time.Minute)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, loginResp{
				MFARequired: true,
				MFAToken:    tok,
				Role:        u.Role,
				Email:       u.Email,
				RedirectTo:  redirectTo,
			})
			return
		}
		if tSvc := h.svc.Throttle(); tSvc != nil {
			reqMFA, err := tSvc.RequiresMFAForRole(r.Context(), u.Role)
			if err != nil {
				writeErr(w, err)
				return
			}
			if reqMFA {
				tok, err := h.svc.User().IssueMFAToken(u, true, 10*time.Minute)
				if err != nil {
					writeErr(w, err)
					return
				}
				writeJSON(w, http.StatusOK, loginResp{
					MFASetupRequired: true,
					MFAToken:         tok,
					Role:             u.Role,
					Email:            u.Email,
					RedirectTo:       redirectTo,
				})
				return
			}
		}
	}

	pair, err := h.svc.User().IssueTokens(u)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResp{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		Role:         pair.Role,
		Email:        u.Email,
		RedirectTo:   redirectTo,
	})
}

// @Summary Verify second-factor TOTP or recovery code during sign-in
// @Router /api/v1/auth/mfa/verify [post]
// @Success 200 {object} loginResp
func (h *Handler) verifyMFAChallenge(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := h.svc.User().VerifyMFAToken(r.Context(), in.MFAToken, auth.TokenUseMFAChallenge)
	if err != nil {
		writeErr(w, err)
		return
	}
	ip := clientIP(r)
	throttleTarget := fmt.Sprintf("mfa:%s", u.Email)
	if err := h.checkLoginThrottle(r, ip, throttleTarget); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := mfaSvc.Verify(r.Context(), u.ID, in.Code); err != nil {
		h.recordLoginFailure(r, ip, throttleTarget)
		auditctx.SetAuditEvent(r, auditctx.Event{
			Action:       auditctx.ActionAuthLoginFailed,
			ResourceType: auditctx.ResourceAuth,
			ResourceID:   strconv.FormatUint(u.ID, 10),
			UserEmail:    u.Email,
			Status:       auditctx.StatusFailure,
			ErrorMessage: "mfa verification failed",
		})
		writeErr(w, err)
		return
	}
	h.recordLoginSuccess(r, throttleTarget)
	pair, err := h.svc.User().IssueTokens(u)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResp{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		Role:         pair.Role,
		Email:        u.Email,
	})
}

// @Summary Begin mandatory MFA enrollment using a login-scoped setup token
// @Router /api/v1/auth/mfa/enroll/start [post]
// @Success 200 {object} map[string]any
func (h *Handler) enrollMFAStart(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in struct {
		MFAToken string `json:"mfa_token"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := h.svc.User().VerifyMFAToken(r.Context(), in.MFAToken, auth.TokenUseMFASetup)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := mfaSvc.Setup(r.Context(), u.ID, u.Email)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary Confirm mandatory MFA enrollment during sign-in and issue session tokens
// @Router /api/v1/auth/mfa/enroll/confirm [post]
// @Success 200 {object} map[string]any
func (h *Handler) enrollMFAConfirm(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	u, err := h.svc.User().VerifyMFAToken(r.Context(), in.MFAToken, auth.TokenUseMFASetup)
	if err != nil {
		writeErr(w, err)
		return
	}
	recoveryCodes, err := mfaSvc.Confirm(r.Context(), u.ID, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	pair, err := h.svc.User().IssueTokens(u)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionUserUpdate,
		ResourceType: auditctx.ResourceUser,
		ResourceID:   strconv.FormatUint(u.ID, 10),
		ResourceName: u.Email,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"field": "mfa", "enabled": true, "flow": "forced_enroll"},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":   pair.AccessToken,
		"refresh_token":  pair.RefreshToken,
		"expires_in":     pair.ExpiresIn,
		"role":           pair.Role,
		"email":          u.Email,
		"recovery_codes": recoveryCodes,
	})
}

// @Summary Get current user's TOTP MFA status
// @Router /api/v1/auth/mfa/status [get]
// @Success 200 {object} map[string]any
func (h *Handler) mfaStatus(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	st, err := mfaSvc.Status(r.Context(), t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	enforced := false
	if tSvc := h.svc.Throttle(); tSvc != nil {
		var policyErr error
		enforced, policyErr = tSvc.RequiresMFAForRole(r.Context(), t.Role)
		if policyErr != nil && h.log != nil {
			h.log.Warn("iam: check MFA policy status failed", "err", policyErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                  st.Enabled,
		"recovery_codes_remaining": st.RecoveryCodesRemaining,
		"verified_at":              st.VerifiedAt,
		"enforced":                 enforced,
	})
}

// @Summary Generate TOTP secret and otpauth URI for current user
// @Router /api/v1/auth/mfa/setup [post]
// @Success 200 {object} map[string]any
func (h *Handler) mfaSetup(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	u, err := h.svc.GetByID(r.Context(), t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := mfaSvc.Setup(r.Context(), u.ID, u.Email)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// @Summary Confirm TOTP enrollment for current user and receive recovery codes
// @Router /api/v1/auth/mfa/confirm [post]
// @Success 200 {object} map[string]any
func (h *Handler) mfaConfirm(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	codes, err := mfaSvc.Confirm(r.Context(), t.UserID, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionUserUpdate,
		ResourceType: auditctx.ResourceUser,
		ResourceID:   strconv.FormatUint(t.UserID, 10),
		ResourceName: t.Email,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"field": "mfa", "enabled": true},
	})
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// @Summary Disable TOTP MFA for current user
// @Router /api/v1/auth/mfa/disable [post]
// @Success 204
func (h *Handler) mfaDisable(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	if tSvc := h.svc.Throttle(); tSvc != nil {
		if required, err := tSvc.RequiresMFAForRole(r.Context(), t.Role); err == nil && required {
			writeErr(w, fmt.Errorf("%w: MFA is mandatory under current auth policy", errs.ErrForbidden))
			return
		}
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := mfaSvc.Disable(r.Context(), t.UserID, in.Code); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionUserUpdate,
		ResourceType: auditctx.ResourceUser,
		ResourceID:   strconv.FormatUint(t.UserID, 10),
		ResourceName: t.Email,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"field": "mfa", "enabled": false},
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Regenerate single-use MFA recovery codes for current user
// @Router /api/v1/auth/mfa/recovery-codes/regenerate [post]
// @Success 200 {object} map[string]any
func (h *Handler) mfaRegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	codes, err := mfaSvc.RegenerateRecoveryCodes(r.Context(), t.UserID, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// @Summary Admin resets MFA for a user
// @Router /api/v1/users/{id}/mfa/reset [post]
// @Success 204
func (h *Handler) adminResetUserMFA(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	mfaSvc := h.svc.MFA()
	if mfaSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := h.svc.GetByID(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	if err := mfaSvc.AdminReset(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionUserUpdate,
		ResourceType: auditctx.ResourceUser,
		ResourceID:   strconv.FormatUint(id, 10),
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"field": "mfa", "reset_by_admin": true},
	})
	w.WriteHeader(http.StatusNoContent)
}

// --- Enterprise SSO ---

type ssoProviderAdminDTO struct {
	ID              uint64    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Enabled         bool      `json:"enabled"`
	IssuerURL       string    `json:"issuer_url,omitempty"`
	AuthURL         string    `json:"auth_url,omitempty"`
	TokenURL        string    `json:"token_url,omitempty"`
	UserInfoURL     string    `json:"userinfo_url,omitempty"`
	JWKSURL         string    `json:"jwks_url,omitempty"`
	ClientID        string    `json:"client_id"`
	HasClientSecret bool      `json:"has_client_secret"`
	CorpID          string    `json:"corp_id,omitempty"`
	AgentID         string    `json:"agent_id,omitempty"`
	Scopes          string    `json:"scopes,omitempty"`
	AutoCreateUser  bool      `json:"auto_create_user"`
	DefaultRole     string    `json:"default_role"`
	DefaultOrgID    *uint64   `json:"default_org_id,omitempty"`
	AllowedDomains  string    `json:"allowed_domains,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toSSOProviderAdminDTO(p *model.SSOProvider) ssoProviderAdminDTO {
	return ssoProviderAdminDTO{
		ID:              p.ID,
		Slug:            p.Slug,
		Name:            p.Name,
		Type:            p.Type,
		Enabled:         p.Enabled,
		IssuerURL:       p.IssuerURL,
		AuthURL:         p.AuthURL,
		TokenURL:        p.TokenURL,
		UserInfoURL:     p.UserInfoURL,
		JWKSURL:         p.JWKSURL,
		ClientID:        p.ClientID,
		HasClientSecret: strings.TrimSpace(p.ClientSecretEnc) != "",
		CorpID:          p.CorpID,
		AgentID:         p.AgentID,
		Scopes:          p.Scopes,
		AutoCreateUser:  p.AutoCreateUser,
		DefaultRole:     p.DefaultRole,
		DefaultOrgID:    p.DefaultOrgID,
		AllowedDomains:  p.AllowedDomains,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

func deriveOrigin(r *http.Request) string {
	scheme := "http"
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = strings.Split(proto, ",")[0]
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host != "" {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	}
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func resolveCallbackURL(r *http.Request, slug, override string) string {
	origin := deriveOrigin(r)
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		if u, err := url.Parse(trimmed); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://") {
			return trimmed
		}
	}
	return origin + "/login/sso/callback/" + url.PathEscape(slug)
}

// @Summary List enabled enterprise SSO providers for login page
// @Router /api/v1/auth/sso/providers [get]
// @Success 200 {object} map[string]any
func (h *Handler) listPublicSSOProviders(w http.ResponseWriter, r *http.Request) {
	items := []sso.PublicProvider{}
	if ssoSvc := h.svc.SSO(); ssoSvc != nil {
		list, err := ssoSvc.ListPublic(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		items = list
	}
	allowLocal := true
	if tSvc := h.svc.Throttle(); tSvc != nil {
		if allowed, err := tSvc.LocalLoginAllowed(r.Context()); err == nil {
			allowLocal = allowed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":                      items,
		"allow_local_password_login": allowLocal,
	})
}

// @Summary Start SSO authorization flow and redirect to identity provider
// @Router /api/v1/auth/sso/{slug}/start [get]
// @Success 200 {object} map[string]any
func (h *Handler) startSSOLogin(w http.ResponseWriter, r *http.Request) {
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	slug := chi.URLParam(r, "slug")
	callbackURL := resolveCallbackURL(r, slug, r.URL.Query().Get("redirect_uri"))
	redirectTo := r.URL.Query().Get("redirect")
	authURL, err := ssoSvc.StartLogin(r.Context(), slug, callbackURL, redirectTo)
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.EqualFold(r.URL.Query().Get("mode"), "json") {
		writeJSON(w, http.StatusOK, map[string]any{"authorize_url": authURL, "callback_url": callbackURL})
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// @Summary Exchange SSO authorization code for OpsPilot session or MFA challenge
// @Router /api/v1/auth/sso/{slug}/exchange [post]
// @Success 200 {object} loginResp
func (h *Handler) exchangeSSOLogin(w http.ResponseWriter, r *http.Request) {
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	slug := chi.URLParam(r, "slug")
	var in struct {
		Code        string `json:"code"`
		State       string `json:"state"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ip := clientIP(r)
	throttleTarget := "sso:" + slug
	if err := h.checkLoginThrottle(r, ip, throttleTarget); err != nil {
		writeErr(w, err)
		return
	}
	callbackURL := resolveCallbackURL(r, slug, in.RedirectURI)
	res, err := ssoSvc.Exchange(r.Context(), slug, in.Code, in.State, callbackURL)
	if err != nil {
		h.recordLoginFailure(r, ip, throttleTarget)
		auditctx.SetAuditEvent(r, auditctx.Event{
			Action:       auditctx.ActionAuthLoginFailed,
			ResourceType: auditctx.ResourceAuth,
			ResourceID:   slug,
			Status:       auditctx.StatusFailure,
			ErrorMessage: err.Error(),
		})
		writeErr(w, err)
		return
	}
	h.completeAuthenticatedLogin(w, r, res.User, res.RedirectTo)
}

// @Summary Forward backend SSO callback parameters to frontend callback page
// @Router /api/v1/auth/sso/{slug}/callback [get]
// @Success 302
func (h *Handler) callbackSSORedirect(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	q := url.Values{}
	for _, k := range []string{"code", "state", "error", "error_description"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	q.Set("via", "api")
	target := "/login/sso/callback/" + url.PathEscape(slug)
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// @Summary List linked external SSO identities for current user
// @Router /api/v1/auth/identities [get]
// @Success 200 {object} map[string]any
func (h *Handler) listMyIdentities(w http.ResponseWriter, r *http.Request) {
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	rows, err := ssoSvc.ListUserIdentities(r.Context(), t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id":             row.ID,
			"provider_id":    row.ProviderID,
			"provider_slug":  row.ProviderSlug,
			"provider_name":  row.ProviderName,
			"provider_type":  row.ProviderType,
			"external_sub":   row.ExternalSub,
			"external_email": row.ExternalEmail,
			"external_name":  row.ExternalName,
			"last_login_at":  row.LastLoginAt,
			"created_at":     row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// @Summary Unlink an external SSO identity for current user
// @Router /api/v1/auth/identities/{id} [delete]
// @Success 204
func (h *Handler) unlinkMyIdentity(w http.ResponseWriter, r *http.Request) {
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	t, ok := tenantctx.From(r.Context())
	if !ok {
		writeErr(w, errs.ErrUnauthorized)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := ssoSvc.UnlinkUserIdentity(r.Context(), t.UserID, id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin Auth Policy & SSO Providers ---

// @Summary Read global authentication and MFA enforcement policy
// @Router /api/v1/iam/auth-policy [get]
// @Success 200 {object} model.AuthPolicy
func (h *Handler) getAuthPolicy(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	tSvc := h.svc.Throttle()
	if tSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	p, err := tSvc.GetPolicy(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mfa_enforce_mode":           p.MFAEnforceMode,
		"allow_local_password_login": p.AllowLocalPasswordLogin,
		"login_ip_limit":             p.LoginIPLimit,
		"login_ip_window_sec":        p.LoginIPWindowSec,
		"login_email_limit":          p.LoginEmailLimit,
		"login_email_window_sec":     p.LoginEmailWindowSec,
		"updated_at":                 p.UpdatedAt,
	})
}

// @Summary Update global authentication and MFA enforcement policy
// @Router /api/v1/iam/auth-policy [put]
// @Success 200 {object} map[string]any
func (h *Handler) updateAuthPolicy(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	tSvc := h.svc.Throttle()
	if tSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in struct {
		MFAEnforceMode          string `json:"mfa_enforce_mode"`
		AllowLocalPasswordLogin bool   `json:"allow_local_password_login"`
		LoginIPLimit            int    `json:"login_ip_limit"`
		LoginIPWindowSec        int    `json:"login_ip_window_sec"`
		LoginEmailLimit         int    `json:"login_email_limit"`
		LoginEmailWindowSec     int    `json:"login_email_window_sec"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	saved, err := tSvc.UpdatePolicy(r.Context(), throttle.PolicyInput{
		MFAEnforceMode:          in.MFAEnforceMode,
		AllowLocalPasswordLogin: in.AllowLocalPasswordLogin,
		LoginIPLimit:            in.LoginIPLimit,
		LoginIPWindowSec:        in.LoginIPWindowSec,
		LoginEmailLimit:         in.LoginEmailLimit,
		LoginEmailWindowSec:     in.LoginEmailWindowSec,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   "auth_policy",
		Status:       auditctx.StatusSuccess,
		Payload: map[string]any{
			"mfa_enforce_mode":           saved.MFAEnforceMode,
			"allow_local_password_login": saved.AllowLocalPasswordLogin,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"mfa_enforce_mode":           saved.MFAEnforceMode,
		"allow_local_password_login": saved.AllowLocalPasswordLogin,
		"login_ip_limit":             saved.LoginIPLimit,
		"login_ip_window_sec":        saved.LoginIPWindowSec,
		"login_email_limit":          saved.LoginEmailLimit,
		"login_email_window_sec":     saved.LoginEmailWindowSec,
		"updated_at":                 saved.UpdatedAt,
	})
}

// @Summary List all enterprise SSO providers (secrets omitted)
// @Router /api/v1/iam/sso/providers [get]
// @Success 200 {object} map[string]any
func (h *Handler) adminListSSOProviders(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	rows, err := ssoSvc.ListAll(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]ssoProviderAdminDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toSSOProviderAdminDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// @Summary Create a new enterprise SSO provider
// @Router /api/v1/iam/sso/providers [post]
// @Success 201 {object} ssoProviderAdminDTO
func (h *Handler) adminCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	var in sso.ProviderInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	created, err := ssoSvc.CreateProvider(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(created.ID, 10),
		ResourceName: created.Slug,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "sso_provider", "type": created.Type, "enabled": created.Enabled},
	})
	writeJSON(w, http.StatusCreated, toSSOProviderAdminDTO(created))
}

// @Summary Update an enterprise SSO provider
// @Router /api/v1/iam/sso/providers/{id} [put]
// @Success 200 {object} ssoProviderAdminDTO
func (h *Handler) adminUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in sso.ProviderInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	updated, err := ssoSvc.UpdateProvider(r.Context(), id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingUpdate,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(updated.ID, 10),
		ResourceName: updated.Slug,
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "sso_provider", "type": updated.Type, "enabled": updated.Enabled},
	})
	writeJSON(w, http.StatusOK, toSSOProviderAdminDTO(updated))
}

// @Summary Delete an enterprise SSO provider
// @Router /api/v1/iam/sso/providers/{id} [delete]
// @Success 204
func (h *Handler) adminDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	ssoSvc := h.svc.SSO()
	if ssoSvc == nil {
		writeErr(w, errs.ErrNotWiredYet)
		return
	}
	id, err := parseID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := ssoSvc.DeleteProvider(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	auditctx.SetAuditEvent(r, auditctx.Event{
		Action:       auditctx.ActionSettingDelete,
		ResourceType: auditctx.ResourceSetting,
		ResourceID:   strconv.FormatUint(id, 10),
		Status:       auditctx.StatusSuccess,
		Payload:      map[string]any{"entity": "sso_provider"},
	})
	w.WriteHeader(http.StatusNoContent)
}
