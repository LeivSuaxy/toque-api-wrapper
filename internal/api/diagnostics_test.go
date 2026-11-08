package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAdminDiagnosticsPage(t *testing.T) {
	env := setupEnv(t, 60, 10)

	_, cookie := env.adminSession(t)
	response := env.request(http.MethodGet, "/admin/diagnostics", nil, nil, []*http.Cookie{cookie})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", response.Header().Get("Cache-Control"))
	}

	body := response.Body.String()
	for _, want := range []string{"Diagnóstico", "Estado de ElToque", "Últimas tasas por moneda", "Peticiones API recientes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}

	unauth := env.request(http.MethodGet, "/admin/diagnostics", nil, nil, nil)
	if unauth.Code != http.StatusSeeOther || unauth.Header().Get("Location") != "/admin/login" {
		t.Fatalf("unauth status = %d, location = %q", unauth.Code, unauth.Header().Get("Location"))
	}
}

func TestAdminDiagnosticsCollectRequiresCSRF(t *testing.T) {
	env := setupEnv(t, 60, 10)

	_, cookie := env.adminSession(t)
	response := env.request(http.MethodPost, "/admin/diagnostics/collect", url.Values{}, nil, []*http.Cookie{cookie})
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}
