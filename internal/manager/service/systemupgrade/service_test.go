package systemupgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckDetectsNewerRelease(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Fatalf("path = %q, want /latest", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
			"tag_name":"v0.8.10",
			"html_url":"https://github.com/Zara1024/OpsPilot/releases/tag/v0.8.10",
			"published_at":"2026-06-10T00:00:00Z"
		}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{
		CurrentVersion: "v0.8.4",
		ReleaseAPIURL:  srv.URL + "/latest",
		DownloadBase:   "https://github.com/Zara1024/OpsPilot/releases/download",
	}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	if !info.ComparisonSupported {
		t.Fatalf("ComparisonSupported = false, want true")
	}
	if info.LatestVersion != "v0.8.10" {
		t.Fatalf("LatestVersion = %q", info.LatestVersion)
	}
	if len(info.Commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(info.Commands))
	}
	if info.Commands[0].ID != "linux-amd64" || info.Commands[1].ID != "linux-arm64" || info.Commands[2].ID != "auto" {
		t.Fatalf("commands = %+v, want amd64, arm64, auto-detect", info.Commands)
	}
	wantAMD64Command := strings.Join([]string{
		"curl -fL -O https://github.com/Zara1024/OpsPilot/releases/download/v0.8.10/opspilot-v0.8.10-linux-amd64.tar.xz || wget https://github.com/Zara1024/OpsPilot/releases/download/v0.8.10/opspilot-v0.8.10-linux-amd64.tar.xz",
		"tar xf opspilot-v0.8.10-linux-amd64.tar.xz && cd opspilot-v0.8.10-linux-amd64",
		"sudo ./upgrade.sh",
	}, "\n")
	if info.Commands[0].Command != wantAMD64Command {
		t.Fatalf("amd64 command = %q, want %q", info.Commands[0].Command, wantAMD64Command)
	}
	if !strings.Contains(info.Commands[2].Command, `PKG="opspilot-v0.8.10-linux-${ARCH}"`) {
		t.Fatalf("auto command does not select an architecture-specific package: %s", info.Commands[2].Command)
	}
}

func TestCheckReportsNoUpdateWhenCurrentMatchesLatest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"tag_name":"v0.8.4"}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{CurrentVersion: "v0.8.4", ReleaseAPIURL: srv.URL}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = true, want false")
	}
	if !info.ComparisonSupported {
		t.Fatalf("ComparisonSupported = false, want true")
	}
}

func TestCheckKeepsDevVersionNonComparable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"tag_name":"v0.8.4"}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{CurrentVersion: "dev", ReleaseAPIURL: srv.URL}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if info.ComparisonSupported {
		t.Fatalf("ComparisonSupported = true, want false")
	}
	if info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = true, want false for non-comparable current")
	}
}

func TestCheckReturnsErrorOnBadReleaseResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{CurrentVersion: "v0.8.4", ReleaseAPIURL: srv.URL}, srv.Client())
	_, err := svc.Check(context.Background())
	if err == nil {
		t.Fatalf("Check returned nil error")
	}
	if !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("error = %v, want HTTP 429", err)
	}
}

func TestCheckUsesOfficialMetadataBeforeGitHubFallback(t *testing.T) {
	t.Parallel()
	var githubCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/latest.json":
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{
				"version":"v0.8.10",
				"release_url":"https://github.com/Zara1024/OpsPilot/releases/tag/v0.8.10",
				"download_base":"https://mirror.example.test/dl"
			}`)); err != nil {
				t.Fatalf("write response: %v", err)
			}
		case "/github/latest":
			githubCalled = true
			http.Error(w, "github should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{
		CurrentVersion: "v0.8.4",
		ReleaseAPIURLs: []string{
			srv.URL + "/dl/latest.json",
			srv.URL + "/github/latest",
		},
	}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if githubCalled {
		t.Fatalf("GitHub fallback was called despite official metadata success")
	}
	if !info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	if !strings.Contains(info.Commands[0].Command, "https://mirror.example.test/dl") {
		t.Fatalf("command does not use metadata download_base: %s", info.Commands[0].Command)
	}
}

func TestCheckFallsBackToGitHubMetadata(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dl/latest.json":
			http.Error(w, "metadata not synced yet", http.StatusNotFound)
		case "/github/latest":
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"tag_name":"v0.8.5"}`)); err != nil {
				t.Fatalf("write response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{
		CurrentVersion: "v0.8.4",
		ReleaseAPIURLs: []string{
			srv.URL + "/dl/latest.json",
			srv.URL + "/github/latest",
		},
	}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if info.LatestVersion != "v0.8.5" {
		t.Fatalf("LatestVersion = %q, want v0.8.5", info.LatestVersion)
	}
	if !info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
}

func TestCheckAcceptsPlainTextVersionMetadata(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("v0.8.6\n")); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{CurrentVersion: "v0.8.4", ReleaseAPIURL: srv.URL}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if info.LatestVersion != "v0.8.6" {
		t.Fatalf("LatestVersion = %q, want v0.8.6", info.LatestVersion)
	}
}

func TestCheckSanitizesLegacyOpspilotCloudDownloadBase(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
			"version":"v1.0.7",
			"release_url":"https://opspilot.cloud/release",
			"download_base":"https://opspilot.cloud/dl"
		}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	svc := New(Config{
		CurrentVersion: "v1.0.6",
		ReleaseAPIURL:  srv.URL,
	}, srv.Client())
	info, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = false, want true")
	}
	wantURL := "https://github.com/Zara1024/OpsPilot/releases/tag/v1.0.7"
	if info.ReleaseURL != wantURL {
		t.Fatalf("ReleaseURL = %q, want %q", info.ReleaseURL, wantURL)
	}
	wantPrefix := "curl -fL -O https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7/opspilot-v1.0.7-linux-amd64.tar.xz"
	if !strings.HasPrefix(info.Commands[0].Command, wantPrefix) {
		t.Fatalf("command = %q, want prefix %q", info.Commands[0].Command, wantPrefix)
	}
}

func TestNormalizeDownloadBase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		base     string
		version  string
		expected string
	}{
		{
			name:     "empty base",
			base:     "",
			version:  "v1.0.7",
			expected: "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "legacy opspilot.cloud",
			base:     "https://opspilot.cloud/dl",
			version:  "v1.0.7",
			expected: "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "github base without tag",
			base:     "https://github.com/Zara1024/OpsPilot/releases/download",
			version:  "v1.0.7",
			expected: "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "github base already with tag",
			base:     "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
			version:  "v1.0.7",
			expected: "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "github base with trailing slash",
			base:     "https://github.com/Zara1024/OpsPilot/releases/download/",
			version:  "v1.0.7",
			expected: "https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "proxy github releases base",
			base:     "https://ghproxy.net/https://github.com/Zara1024/OpsPilot/releases/download",
			version:  "v1.0.7",
			expected: "https://ghproxy.net/https://github.com/Zara1024/OpsPilot/releases/download/v1.0.7",
		},
		{
			name:     "custom third party base",
			base:     "https://myrepo.local/dist",
			version:  "v1.0.7",
			expected: "https://myrepo.local/dist",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeDownloadBase(tt.base, tt.version)
			if got != tt.expected {
				t.Fatalf("normalizeDownloadBase(%q, %q) = %q, want %q", tt.base, tt.version, got, tt.expected)
			}
		})
	}
}

