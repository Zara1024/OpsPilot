package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zara1024/OpsPilot/internal/pkg/config"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
)

func TestBuildReadyHandler(t *testing.T) {
	gdb, err := dbx.Open(config.DBConfig{Dialect: "sqlite", Path: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("dbx.Open sqlite: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("gorm.DB.DB(): %v", err)
	}

	// 1. 数据库可用且根上下文活跃 -> 200 ready
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := buildReadyHandler(ctx, sqlDB)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ready" {
		t.Fatalf("healthy readyz = (%d, %q), want (200, ready)", rec.Code, rec.Body.String())
	}

	// 2. 根上下文取消后立即进入 draining 状态 -> 503 draining
	cancel()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "draining" {
		t.Fatalf("draining readyz = (%d, %q), want (503, draining)", rec.Code, rec.Body.String())
	}

	// 3. 数据库连接关闭或缺失 -> 503 db_unavailable
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sqlDB: %v", err)
	}
	hClosed := buildReadyHandler(context.Background(), sqlDB)
	rec = httptest.NewRecorder()
	hClosed.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "db_unavailable" {
		t.Fatalf("closed db readyz = (%d, %q), want (503, db_unavailable)", rec.Code, rec.Body.String())
	}

	hNil := buildReadyHandler(context.Background(), nil)
	rec = httptest.NewRecorder()
	hNil.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "db_unavailable" {
		t.Fatalf("nil db readyz = (%d, %q), want (503, db_unavailable)", rec.Code, rec.Body.String())
	}
}
