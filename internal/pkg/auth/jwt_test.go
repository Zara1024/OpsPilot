package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignerTokenUseAndMiddlewareIsolation(t *testing.T) {
	signer := NewSigner("test-secret-for-jwt-isolation-unit-test", 15*time.Minute, 24*time.Hour)
	base := Claims{
		UserID:      7,
		Email:       "alice@example.com",
		Role:        "user",
		IsSuperuser: false,
	}

	accessTok, err := signer.SignAccess(base)
	require.NoError(t, err)
	refreshTok, err := signer.SignRefresh(base)
	require.NoError(t, err)
	mfaTok, err := signer.SignMFAChallenge(base, 5*time.Minute)
	require.NoError(t, err)
	setupTok, err := signer.SignMFASetup(base, 10*time.Minute)
	require.NoError(t, err)
	legacyTok, err := signer.SignWithTTL(base, 5*time.Minute)
	require.NoError(t, err)

	accessClaims, err := signer.Verify(accessTok)
	require.NoError(t, err)
	require.Equal(t, TokenUseAccess, accessClaims.TokenUse)

	refreshClaims, err := signer.Verify(refreshTok)
	require.NoError(t, err)
	require.Equal(t, TokenUseRefresh, refreshClaims.TokenUse)

	handler := Middleware(signer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"access token allowed", accessTok, http.StatusOK},
		{"legacy token without token_use allowed", legacyTok, http.StatusOK},
		{"refresh token rejected on API", refreshTok, http.StatusUnauthorized},
		{"mfa challenge token rejected on API", mfaTok, http.StatusUnauthorized},
		{"mfa setup token rejected on API", setupTok, http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code)
		})
	}
}
