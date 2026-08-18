package api

import (
	"net/http/httptest"
	"testing"
)

func TestDeviceFilterAcceptsKnownPlatform(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/devices?platform=IOS", nil)
	filter, ok := deviceFilter(request)
	if !ok || filter.Platform != "ios" {
		t.Fatalf("deviceFilter() = %+v, %v", filter, ok)
	}
}

func TestDeviceFilterRejectsUnknownPlatform(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/devices?platform=windows", nil)
	if _, ok := deviceFilter(request); ok {
		t.Fatal("deviceFilter() accepted an unknown platform")
	}
}
