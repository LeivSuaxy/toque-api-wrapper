package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderTemplates(t *testing.T) {
	templates, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, 200, "login.html", map[string]any{"Title": "Acceso"}); err != nil {
		t.Fatalf("render login: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "superadministrador") {
		t.Fatalf("login body = %s", recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	if err := templates.Render(recorder, 200, "dashboard.html", map[string]any{
		"Title":    "API keys",
		"Username": "admin",
		"CSRF":     "token",
	}); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "API keys") {
		t.Fatalf("dashboard body missing title")
	}
}

func TestHelpers(t *testing.T) {
	if got := humanBytes(512); got != "512 B" {
		t.Fatalf("humanBytes = %q", got)
	}
	if got := humanBytes(2048); got != "2.0 KB" {
		t.Fatalf("humanBytes = %q", got)
	}
	if got := maskKey("tk_live_abcdefghijklmnop"); !strings.Contains(got, "…") {
		t.Fatalf("maskKey = %q", got)
	}
	if got := formatDate(""); got != "—" {
		t.Fatalf("formatDate empty = %q", got)
	}
}
