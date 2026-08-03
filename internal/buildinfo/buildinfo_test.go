package buildinfo

import (
	"strings"
	"testing"
)

func TestStringContainsStableFields(t *testing.T) {
	got := String("device-farm-server")

	for _, want := range []string{
		"device-farm-server",
		"version=",
		"commit=",
		"build_date=",
		"go=",
		"os=",
		"arch=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, want field %q", got, want)
		}
	}

	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("String() must stay on one line, got %q", got)
	}
}

func TestNormalizeUsesFallbackForBlankValue(t *testing.T) {
	if got := normalize("   ", "fallback"); got != "fallback" {
		t.Fatalf("normalize() = %q, want fallback", got)
	}
}
