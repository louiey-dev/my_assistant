package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesDashboardRoot(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	res := httptest.NewRecorder()
	Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("dashboard root status = %d, want %d", res.Code, http.StatusOK)
	}
	if location := res.Header().Get("Location"); location != "" {
		t.Fatalf("dashboard root unexpectedly redirects to %q", location)
	}
}
