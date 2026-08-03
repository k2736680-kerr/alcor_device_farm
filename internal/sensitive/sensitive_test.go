package sensitive

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactsTextAndNestedPayloadSecrets(t *testing.T) {
	payload := RedactMap(map[string]any{
		"authorization": "Bearer top-secret-token", "safe": "visible",
		"nested": map[string]any{"message": "password=hunter2", "database": "postgres://user:pass@db/internal"},
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"top-secret-token", "hunter2", "user:pass"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted payload contains %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, Redacted) || !strings.Contains(text, "visible") {
		t.Fatalf("payload=%s", text)
	}
}

func TestContainsOnlyFlagsSecretShapedText(t *testing.T) {
	if !Contains("rotation failed: token=abc123456") || !Contains("Authorization Bearer abcdef123456") ||
		!Contains("http://operator:password@appium.internal/status") {
		t.Fatal("secret-shaped text was not detected")
	}
	if Contains("operator rotated the service token after maintenance") {
		t.Fatal("ordinary reason text was incorrectly rejected")
	}
}

func TestContainsMapFindsSensitiveKeysAndNestedValues(t *testing.T) {
	if !ContainsMap(map[string]any{"nested": []any{map[string]any{"message": "password=hunter2"}}}) ||
		!ContainsMap(map[string]any{"api_token": "opaque-value"}) {
		t.Fatal("nested or keyed secret was not detected")
	}
	if ContainsMap(map[string]any{"apiLevel": 34, "labels": []any{"android", "emulator"}}) {
		t.Fatal("safe capability map was incorrectly rejected")
	}
}
