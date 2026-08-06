package consoleui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesEntryAndAssetsWithCachePolicy(t *testing.T) {
	handler := Handler()

	// Discover a real content-hashed asset embedded in dist so the immutable
	// cache test does not depend on a fixed file name.
	embeddedDist, err := fs.Sub(assets, "dist")
	if err != nil {
		t.Fatalf("embedded dist unavailable: %v", err)
	}
	assetDir, err := fs.Sub(embeddedDist, "assets")
	if err != nil {
		t.Fatalf("no assets dir in embedded dist: %v", err)
	}
	entries, err := fs.ReadDir(assetDir, ".")
	if err != nil || len(entries) == 0 {
		t.Fatalf("embedded dist has no assets: %v", err)
	}
	assetName := entries[0].Name()

	t.Run("index.html is served with no-store", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/console/", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /console/ = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("index.html Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("SPA fallback route is served with no-store", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/console/reservations", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /console/reservations = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("SPA fallback Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("content-hashed asset is cached immutably", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/console/assets/"+assetName, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /console/assets/%s = %d, want 200", assetName, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("asset Cache-Control = %q, want immutable long cache", got)
		}
	})

	t.Run("security headers are present on every response", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/console/", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		for name, want := range map[string]string{
			"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
			"Referrer-Policy":         "no-referrer",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		} {
			if got := recorder.Header().Get(name); got != want {
				t.Errorf("header %s = %q, want %q", name, got, want)
			}
		}
	})
}
