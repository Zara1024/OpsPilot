package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Zara1024/OpsPilot/internal/iam/service"
)

// 测试超长密码在入口层被拦截并返回 400 Bad Request
func TestLogin_RejectsLongPassword_BadRequest(t *testing.T) {
	h := NewHandler(&service.Service{}, nil)
	r := chi.NewRouter()
	h.RegisterPublic(r)

	reqBody, _ := json.Marshal(loginReq{
		Email:    "test@example.com",
		Password: strings.Repeat("A", 10000),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// 测试触发 429 时响应头包含 Retry-After
func TestLogin_RateLimit_ContainsRetryAfter(t *testing.T) {
	h := NewHandler(&service.Service{}, nil)
	r := chi.NewRouter()
	h.RegisterPublic(r)

	// 人为填充 throttle 达到上限
	ip := "192.168.1.100"
	email := "test@example.com"
	for i := 0; i < 10; i++ {
		h.throttle.recordFailure(ip, email)
	}

	reqBody, _ := json.Marshal(loginReq{
		Email:    email,
		Password: "password123",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", w.Code)
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Fatalf("expected Retry-After: 60, got %q", retryAfter)
	}
}

// 测试未授权请求注册接口返回 401 且不泄露信息
func TestRegister_UnauthorizedWithoutAdmin(t *testing.T) {
	h := NewHandler(&service.Service{}, nil)
	r := chi.NewRouter()
	h.RegisterProtected(r)

	reqBody, _ := json.Marshal(registerReq{
		Email:    "newuser@example.com",
		Password: "password123",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
