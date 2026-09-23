package contract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDeviceDomainMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000001_device_domain.up.sql"))
	down := readFile(t, filepath.Join(root, "000001_device_domain.down.sql"))

	tables := []string{
		"device_images", "device_hosts", "device_host_commands", "device_pools",
		"device_pool_images", "devices", "device_pool_devices", "device_reservations",
		"device_sessions", "device_health_events", "device_audit_events",
	}
	for _, table := range tables {
		assertSQLContains(t, up, `CREATE\s+TABLE\s+`+table+`\b`)
		assertSQLContains(t, down, `DROP\s+TABLE\s+IF\s+EXISTS\s+`+table+`\b`)
	}

	for _, forbidden := range []string{"eval_tasks", "eval_results", "runs", "run_attempts", "run_results", "artifacts"} {
		assertSQLAbsent(t, up, `\b`+forbidden+`\b`)
	}
	assertSQLContains(t, up, `CREATE\s+UNIQUE\s+INDEX\s+uq_device_reservations_active_device`)
	assertSQLContains(t, up, `UNIQUE\s*\(client_id,\s*idempotency_key\)`)
	assertSQLContains(t, up, `serial\s+varchar\(255\)\s+NOT\s+NULL\s+UNIQUE`)
}

func TestAPIIdempotencyMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000002_api_idempotency.up.sql"))
	down := readFile(t, filepath.Join(root, "000002_api_idempotency.down.sql"))
	assertSQLContains(t, up, `CREATE\s+TABLE\s+device_idempotency_records\b`)
	assertSQLContains(t, up, `PRIMARY\s+KEY\s*\(client_id,\s*scope,\s*idempotency_key\)`)
	assertSQLContains(t, down, `DROP\s+TABLE\s+IF\s+EXISTS\s+device_idempotency_records\b`)
}

func TestImageValidationCommandMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000003_image_validation_command.up.sql"))
	down := readFile(t, filepath.Join(root, "000003_image_validation_command.down.sql"))
	assertSQLContains(t, up, `validate_image`)
	assertSQLAbsent(t, down, `validate_image`)
}

func TestAndroidSystemImageCatalogMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000009_android_system_image_catalog.up.sql"))
	down := readFile(t, filepath.Join(root, "000009_android_system_image_catalog.down.sql"))
	for _, table := range []string{"android_system_image_catalog", "device_image_preparations"} {
		assertSQLContains(t, up, `CREATE\s+TABLE\s+`+table+`\b`)
		assertSQLContains(t, down, `DROP\s+TABLE\s+IF\s+EXISTS\s+`+table+`\b`)
	}
	assertSQLContains(t, up, `catalog_revision`)
	assertSQLContains(t, up, `build_command_id`)
	assertSQLContains(t, up, `validation_command_id`)
	assertSQLContains(t, up, `status\s+<>\s+'cached'\s+OR\s+image_id\s+IS\s+NOT\s+NULL`)
	assertSQLContains(t, up, `uq_device_images_digest_runtime_profile`)
	assertSQLContains(t, down, `device_images_docker_digest_key`)
}

func TestVerifiedImagePreparationCompatibilityMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000010_verified_image_preparation_gate.up.sql"))
	down := readFile(t, filepath.Join(root, "000010_verified_image_preparation_gate.down.sql"))
	assertSQLContains(t, up, `RENAME\s+COLUMN\s+command_id\s+TO\s+build_command_id`)
	assertSQLContains(t, up, `ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+validation_command_id`)
	assertSQLContains(t, up, `status\s+<>\s+'cached'\s+OR\s+image_id\s+IS\s+NOT\s+NULL`)
	assertSQLContains(t, up, `api_level\s+BETWEEN\s+33\s+AND\s+36`)
	assertSQLContains(t, down, `DROP\s+COLUMN\s+IF\s+EXISTS\s+validation_command_id`)
}

func TestPlatformNeutralDeviceDomainMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000014_platform_neutral_device_domain.up.sql"))
	down := readFile(t, filepath.Join(root, "000014_platform_neutral_device_domain.down.sql"))

	for _, expected := range []string{
		`ADD\s+COLUMN\s+host_os`, `ADD\s+COLUMN\s+host_arch`,
		`ADD\s+COLUMN\s+platform`, `appium_device_farm_ios`, `simulator`,
		`CREATE\s+TRIGGER\s+trg_device_pool_devices_platform`,
		`DROP\s+INDEX\s+uq_devices_active_appium_endpoint`,
		`CREATE\s+UNIQUE\s+INDEX\s+uq_devices_active_android_appium_endpoint`,
		`CREATE\s+INDEX\s+ix_devices_appium_endpoint`,
		`CREATE\s+OR\s+REPLACE\s+FUNCTION\s+device_schedulable_capabilities`,
	} {
		assertSQLContains(t, up, expected)
	}
	assertSQLContains(t, down, `cannot\s+downgrade\s+platform-neutral\s+device\s+domain`)
	assertSQLContains(t, down, `CREATE\s+UNIQUE\s+INDEX\s+uq_devices_active_appium_endpoint`)
	assertSQLContains(t, down, `DROP\s+COLUMN\s+platform`)
}

func TestIOSSessionFenceMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000015_ios_session_fence.up.sql"))
	down := readFile(t, filepath.Join(root, "000015_ios_session_fence.down.sql"))

	for _, expected := range []string{
		`ADD\s+COLUMN\s+session_grant_hash`, `ADD\s+COLUMN\s+session_grant_expires_at`,
		`ADD\s+COLUMN\s+session_grant_consumed_at`, `ADD\s+COLUMN\s+appium_session_id`,
		`CREATE\s+UNIQUE\s+INDEX\s+uq_device_sessions_grant_hash`,
		`CREATE\s+UNIQUE\s+INDEX\s+uq_device_sessions_active_appium_device`,
		`session_fence`,
	} {
		assertSQLContains(t, up, expected)
	}
	for _, expected := range []string{
		`DROP\s+INDEX\s+IF\s+EXISTS\s+uq_device_sessions_active_appium_device`,
		`DROP\s+COLUMN\s+IF\s+EXISTS\s+session_grant_hash`,
		`DROP\s+COLUMN\s+IF\s+EXISTS\s+appium_session_id`,
	} {
		assertSQLContains(t, down, expected)
	}
}

func TestIOSRemoteControlDoesNotAddHostWindowCommands(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000017_ios_remote_control_fence.up.sql"))
	down := readFile(t, filepath.Join(root, "000017_ios_remote_control_fence.down.sql"))

	for _, commandType := range []string{"remote_open", "remote_close", "remote_health"} {
		assertSQLAbsent(t, up, commandType)
		assertSQLAbsent(t, down, commandType)
	}
	for _, existingType := range []string{"validate_image", "sync_android_catalog", "prepare_android_image"} {
		assertSQLContains(t, up, existingType)
		assertSQLContains(t, down, existingType)
	}
}

func TestDeviceRuntimeProfileUpdateMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	up := readFile(t, filepath.Join(root, "000019_device_runtime_profile_update.up.sql"))
	down := readFile(t, filepath.Join(root, "000019_device_runtime_profile_update.down.sql"))
	for _, expected := range []string{
		`ADD\s+COLUMN\s+runtime_profile_update_status`,
		`ADD\s+COLUMN\s+runtime_profile_update_error`,
		`CREATE\s+INDEX\s+ix_devices_runtime_profile_update_pending`,
		`runtime_profile_update_status\s*=\s*'pending'`,
	} {
		assertSQLContains(t, up, expected)
	}
	assertSQLContains(t, down, `DROP\s+COLUMN\s+IF\s+EXISTS\s+runtime_profile_update_status`)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func assertSQLContains(t *testing.T, sql, pattern string) {
	t.Helper()
	if !regexp.MustCompile(`(?i)` + pattern).MatchString(sql) {
		t.Fatalf("SQL does not match %q", pattern)
	}
}

func assertSQLAbsent(t *testing.T, sql, pattern string) {
	t.Helper()
	if regexp.MustCompile(`(?i)` + pattern).MatchString(sql) {
		t.Fatalf("SQL unexpectedly matches %q", pattern)
	}
	if strings.TrimSpace(sql) == "" {
		t.Fatal("SQL is empty")
	}
}
