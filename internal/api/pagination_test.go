package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

func TestPaginationDefaultsToTheFirstPage(t *testing.T) {
	page, ok := pagination(httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	if !ok {
		t.Fatal("a request without pagination parameters must be accepted")
	}
	if page.Number() != 1 || page.Size() != paging.DefaultSize {
		t.Fatalf("page = %d/%d, want 1/%d", page.Number(), page.Size(), paging.DefaultSize)
	}
}

func TestPaginationRejectsUnusableParameters(t *testing.T) {
	for _, query := range []string{
		"?page=0", "?page=-1", "?page=abc", "?page=1.5",
		"?page_size=0", "?page_size=201", "?page_size=-5", "?page_size=many",
	} {
		t.Run(query, func(t *testing.T) {
			if _, ok := pagination(httptest.NewRequest(http.MethodGet, "/api/v1/devices"+query, nil)); ok {
				t.Fatalf("%s must be rejected instead of silently clamped", query)
			}
		})
	}
}

func TestPaginationPushesTheRequestedWindowDown(t *testing.T) {
	page, ok := pagination(httptest.NewRequest(http.MethodGet, "/api/v1/devices?page=3&page_size=20", nil))
	if !ok {
		t.Fatal("page=3&page_size=20 must be accepted")
	}
	if page.Limit() != 20 || page.Offset() != 40 {
		t.Fatalf("limit=%d offset=%d, want 20/40", page.Limit(), page.Offset())
	}
}
