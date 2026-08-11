package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imagecatalog"
)

const (
	buildHostID = "host_build_agent_000001"
	digestOne   = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digestTwo   = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
)

func TestOfficialCatalogBuildValidationAndDigestCacheFlow(t *testing.T) {
	environment := newManagementEnvironment(t)
	ctx := context.Background()
	if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_hosts
		(id,name,host_type,status,draining,capabilities,capacity)
		VALUES($1,'official-image-build-agent','docker_emulator','online',false,
		'{"image_build_agent":true,"kvm":true}'::jsonb,
		'{"cpu_cores":12,"memory_total_mb":16384,"memory_available_mb":12288,"disk_total_mb":200000,"disk_available_mb":150000,"device_slots":2,"resource_model":"dynamic"}'::jsonb)`, buildHostID); err != nil {
		t.Fatal(err)
	}

	syncResponse := environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/synchronizations", nil,
		serviceToken, "catalog-sync-0001", "integration-image-admin")
	assertStatus(t, syncResponse, http.StatusAccepted)
	completeCatalogCommand(t, environment, "sync_android_catalog", map[string]any{"entries": []map[string]any{
		{"package_name": "system-images;android-36;google_apis;x86_64", "api_level": 36, "image_type": "google_apis", "abi": "x86_64", "revision": "16"},
		{"package_name": "system-images;android-36;google_play;x86_64", "api_level": 36, "image_type": "google_play", "abi": "x86_64", "revision": "8"},
	}})

	listResponse := environment.request(t, http.MethodGet, "/api/v1/android-system-images", nil, serviceToken, "")
	assertStatus(t, listResponse, http.StatusOK)
	var entries []imagecatalog.Entry
	decodeData(t, listResponse, &entries)
	if len(entries) != 2 || entries[0].Status != "downloadable" {
		t.Fatalf("catalog entries=%#v", entries)
	}
	catalogID := entries[0].ID

	prepareBody := map[string]any{"catalog_id": catalogID, "runtime_profile": map[string]any{
		"container_cpu_cores": 4, "container_memory_mb": 5120, "guest_cpu_cores": 4, "guest_memory_mb": 4096,
		"data_disk_mb": 4096, "width": 1080, "height": 2400, "density_dpi": 420, "vm_heap_mb": 512, "graphics": "auto",
	}}
	prepareResponse := environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/preparations", prepareBody,
		serviceToken, "catalog-prepare-0001", "integration-image-admin")
	assertStatus(t, prepareResponse, http.StatusAccepted)
	assertImageCount(t, environment, 0)

	build := claimCatalogCommand(t, environment, "prepare_android_image")
	if build.Payload["package_name"] != "system-images;android-36;google_apis;x86_64" || build.Payload["revision"] != "16" {
		t.Fatalf("untrusted or unpinned build payload=%#v", build.Payload)
	}
	completeClaimedCatalogCommand(t, environment, build, map[string]any{
		"docker_image":  "registry.example/alcor/android-emulator:api36-google_apis-x86_64-r16",
		"docker_digest": digestOne, "image_disk_mb": 8192,
	})
	// A successful build is not enough: no usable Device Image exists until a
	// real boot/ADB/Appium/STF validation command succeeds.
	assertImageCount(t, environment, 0)
	validation := claimCatalogCommand(t, environment, "validate_image")
	if validation.Payload["docker_digest"] != digestOne || validation.Payload["operation_source"] != "android_catalog" {
		t.Fatalf("validation payload=%#v", validation.Payload)
	}
	completeClaimedCatalogCommand(t, environment, validation, map[string]any{
		"digest_verified": true, "ready": true, "stf_registered": true,
		"adb_endpoint": "10.0.30.171:31000", "appium_endpoint": "http://10.0.30.171:32000",
	})
	assertImageCount(t, environment, 1)

	listResponse = environment.request(t, http.MethodGet, "/api/v1/android-system-images", nil, serviceToken, "")
	assertStatus(t, listResponse, http.StatusOK)
	decodeData(t, listResponse, &entries)
	if entries[0].Status != "cached" || entries[0].ImageID == "" {
		t.Fatalf("validated catalog entry=%#v", entries[0])
	}

	// A distinct request may rebuild, but the identical immutable digest and
	// runtime profile reuse the already validated row without a second boot.
	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/preparations", prepareBody,
		serviceToken, "catalog-prepare-0002", "integration-image-admin"), http.StatusAccepted)
	secondBuild := claimCatalogCommand(t, environment, "prepare_android_image")
	completeClaimedCatalogCommand(t, environment, secondBuild, map[string]any{
		"docker_image":  "registry.example/alcor/android-emulator:api36-google_apis-x86_64-r16",
		"docker_digest": digestOne, "image_disk_mb": 8192,
	})
	assertImageCount(t, environment, 1)
	commands, err := environment.hostCommands.Claim(ctx, buildHostID, hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(commands) != 0 {
		t.Fatalf("identical digest unexpectedly queued validation: %#v error=%v", commands, err)
	}

	var cachedPreparations, auditRows int
	if err := environment.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_image_preparations WHERE status='cached' AND image_id IS NOT NULL`).Scan(&cachedPreparations); err != nil {
		t.Fatal(err)
	}
	if err := environment.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_audit_events
		WHERE actor_id='integration-image-admin' AND action IN ('synchronize_android_system_images','prepare_android_system_image')`).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if cachedPreparations != 2 || auditRows != 3 {
		t.Fatalf("cached preparations=%d audit rows=%d", cachedPreparations, auditRows)
	}

	failedBody := map[string]any{"catalog_id": catalogID, "runtime_profile": map[string]any{
		"container_cpu_cores": 4, "container_memory_mb": 6144, "guest_cpu_cores": 4, "guest_memory_mb": 5120,
		"data_disk_mb": 4096, "width": 1080, "height": 2400, "density_dpi": 420, "vm_heap_mb": 512, "graphics": "auto",
	}}
	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/preparations", failedBody,
		serviceToken, "catalog-prepare-0003", "integration-image-admin"), http.StatusAccepted)
	failedBuild := claimCatalogCommand(t, environment, "prepare_android_image")
	if _, err := environment.hostCommands.Complete(ctx, failedBuild.ID, hostcommand.CompletionInput{
		LeaseToken: *failedBuild.LeaseToken, Attempt: failedBuild.Attempt, Status: "failed",
		Error: &hostcommand.CompletionError{Code: "SDK_DOWNLOAD_FAILED", Message: "injected official SDK failure", Retryable: false},
	}); err != nil {
		t.Fatalf("complete failed build: %v", err)
	}
	assertImageCount(t, environment, 1)
	var failedPreparations int
	if err := environment.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_image_preparations WHERE status='failed' AND error_code='SDK_DOWNLOAD_FAILED'`).Scan(&failedPreparations); err != nil {
		t.Fatal(err)
	}
	if failedPreparations != 1 {
		t.Fatalf("failed preparations=%d", failedPreparations)
	}

	missingSTFBody := map[string]any{"catalog_id": catalogID, "runtime_profile": map[string]any{
		"container_cpu_cores": 4, "container_memory_mb": 7168, "guest_cpu_cores": 4, "guest_memory_mb": 6144,
		"data_disk_mb": 4096, "width": 1080, "height": 2400, "density_dpi": 420, "vm_heap_mb": 512, "graphics": "auto",
	}}
	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/preparations", missingSTFBody,
		serviceToken, "catalog-prepare-0004", "integration-image-admin"), http.StatusAccepted)
	missingSTFBuild := claimCatalogCommand(t, environment, "prepare_android_image")
	completeClaimedCatalogCommand(t, environment, missingSTFBuild, map[string]any{
		"docker_image":  "registry.example/alcor/android-emulator:api36-google_apis-x86_64-r16-stf-check",
		"docker_digest": digestTwo, "image_disk_mb": 9000,
	})
	missingSTFValidation := claimCatalogCommand(t, environment, "validate_image")
	completeClaimedCatalogCommand(t, environment, missingSTFValidation, map[string]any{
		"digest_verified": true, "ready": true, "stf_registered": false,
	})
	assertImageCount(t, environment, 1)
	var incompleteValidations int
	if err := environment.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_image_preparations
		WHERE status='failed' AND error_code='IMAGE_VALIDATION_INCOMPLETE'`).Scan(&incompleteValidations); err != nil {
		t.Fatal(err)
	}
	if incompleteValidations != 1 {
		t.Fatalf("incomplete STF validations=%d", incompleteValidations)
	}

	assertStatus(t, environment.requestAsActor(t, http.MethodPost, "/api/v1/android-system-images/synchronizations", nil,
		serviceToken, "catalog-sync-0002", "integration-image-admin"), http.StatusAccepted)
	completeCatalogCommand(t, environment, "sync_android_catalog", map[string]any{"entries": []map[string]any{
		{"package_name": "system-images;android-36;google_apis;x86_64", "api_level": 36, "image_type": "google_apis", "abi": "x86_64", "revision": "17"},
		{"package_name": "system-images;android-36;google_play;x86_64", "api_level": 36, "image_type": "google_play", "abi": "x86_64", "revision": "8"},
	}})
	listResponse = environment.request(t, http.MethodGet, "/api/v1/android-system-images", nil, serviceToken, "")
	assertStatus(t, listResponse, http.StatusOK)
	decodeData(t, listResponse, &entries)
	if entries[0].Revision != "17" || entries[0].Status != "official_updated" {
		t.Fatalf("official update was not surfaced: %#v", entries[0])
	}
}

func claimCatalogCommand(t *testing.T, environment *managementEnvironment, commandType string) hostcommand.Command {
	t.Helper()
	commands, err := environment.hostCommands.Claim(context.Background(), buildHostID, hostcommand.ClaimInput{LeaseSeconds: 30, MaxCommands: 1})
	if err != nil || len(commands) != 1 || commands[0].CommandType != commandType || commands[0].LeaseToken == nil {
		t.Fatalf("claim %s command=%#v error=%v", commandType, commands, err)
	}
	return commands[0]
}

func completeCatalogCommand(t *testing.T, environment *managementEnvironment, commandType string, result map[string]any) {
	t.Helper()
	completeClaimedCatalogCommand(t, environment, claimCatalogCommand(t, environment, commandType), result)
}

func completeClaimedCatalogCommand(t *testing.T, environment *managementEnvironment, command hostcommand.Command, result map[string]any) {
	t.Helper()
	if _, err := environment.hostCommands.Complete(context.Background(), command.ID, hostcommand.CompletionInput{
		LeaseToken: *command.LeaseToken, Attempt: command.Attempt, Status: "succeeded", Result: result,
	}); err != nil {
		t.Fatalf("complete %s: %v", command.CommandType, err)
	}
}

func assertImageCount(t *testing.T, environment *managementEnvironment, want int) {
	t.Helper()
	var count int
	if err := environment.db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM device_images`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("device image count=%d want=%d", count, want)
	}
}
