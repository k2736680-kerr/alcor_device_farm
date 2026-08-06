package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleLocalE2ERunnerIsPortableAndTestDatabaseOnly(t *testing.T) {
	runner, err := os.ReadFile(filepath.Join("..", "..", "scripts", "run-console-local-e2e.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	rawRunner := string(runner)
	for _, required := range []string{
		"127.0.0.1", "localhost", "^device_farm_", "e2e/fixtures", "seed.sql",
		"DEVICE_FARM_E2E_SERVER_BINARY", "DEVICE_FARM_E2E_REUSE_SERVER", "open reservations",
	} {
		if !strings.Contains(rawRunner, required) {
			t.Fatalf("Console local E2E runner is missing %q", required)
		}
	}

	playwright, err := os.ReadFile(filepath.Join("..", "..", "console", "playwright.config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	rawPlaywright := string(playwright)
	if strings.Contains(rawPlaywright, "AutoTestTools") ||
		!strings.Contains(rawPlaywright, "DEVICE_FARM_E2E_SERVER_BINARY") ||
		!strings.Contains(rawPlaywright, "DEVICE_FARM_E2E_SERVER_CONFIG") {
		t.Fatalf("Playwright Server launch still depends on a developer-specific path:\n%s", rawPlaywright)
	}

	for _, fixture := range []string{"console-users.yaml", "seed.sql", "mock-stf.mjs"} {
		if _, err := os.Stat(filepath.Join("..", "..", "console", "e2e", "fixtures", fixture)); err != nil {
			t.Fatalf("Console local E2E fixture %s is missing: %v", fixture, err)
		}
	}
}
