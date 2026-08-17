package contract

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var expectedOperations = map[string][]string{
	"/healthz":                                               {"get"},
	"/readyz":                                                {"get"},
	"/metrics":                                               {"get"},
	"/api/v1/device-images":                                  {"get", "post"},
	"/api/v1/device-images/{id}":                             {"get", "put"},
	"/api/v1/device-images/{id}/validations":                 {"post"},
	"/api/v1/device-images/{id}/retirements":                 {"post"},
	"/api/v1/android-system-images":                          {"get"},
	"/api/v1/android-system-images/synchronizations":         {"post"},
	"/api/v1/android-system-images/preparations":             {"post"},
	"/api/v1/android-hardware-profiles":                      {"get"},
	"/api/v1/device-provisionings":                           {"get", "post"},
	"/api/v1/device-provisionings/{id}":                      {"get"},
	"/api/v1/device-hosts":                                   {"get", "post"},
	"/api/v1/device-hosts/{id}":                              {"get", "put"},
	"/api/v1/device-hosts/{id}/drains":                       {"post", "delete"},
	"/api/v1/device-pools":                                   {"get", "post"},
	"/api/v1/device-pools/{id}":                              {"get", "put"},
	"/api/v1/device-pools/{id}/default-image":                {"put"},
	"/api/v1/device-pools/{id}/base-device":                  {"put"},
	"/api/v1/device-pools/{id}/devices":                      {"post", "delete"},
	"/api/v1/device-pools/{id}/images":                       {"get"},
	"/api/v1/device-pools/{id}/images/{image_id}":            {"put", "delete"},
	"/api/v1/devices":                                        {"get"},
	"/api/v1/devices/{id}":                                   {"get", "delete"},
	"/api/v1/devices/{id}/restarts":                          {"post"},
	"/api/v1/devices/{id}/rebuilds":                          {"post"},
	"/api/v1/devices/{id}/reimages":                          {"post"},
	"/api/v1/devices/{id}/quarantines":                       {"post", "delete"},
	"/api/v1/devices/{id}/health-events":                     {"get"},
	"/api/v1/devices/{id}/remote-control":                    {"get", "post", "delete"},
	"/api/v1/devices/{id}/remote-control/heartbeat":          {"post"},
	"/api/v1/device-reservations":                            {"get", "post"},
	"/api/v1/device-reservations/{id}":                       {"get"},
	"/api/v1/device-reservations/{id}/extensions":            {"post"},
	"/api/v1/device-reservations/{id}/releases":              {"post"},
	"/api/v1/device-reservations/{id}/remote-sessions":       {"post"},
	"/api/v1/device-reservations/{id}/session-grants":        {"post"},
	"/api/v1/device-audit-events":                            {"get"},
	"/console/api/v1/sessions":                               {"post"},
	"/console/api/v1/me":                                     {"get"},
	"/console/api/v1/sessions/current":                       {"delete"},
	"/console/api/v1/devices/{id}/remote-control":            {"get", "post", "delete"},
	"/console/api/v1/devices/{id}/remote-control/heartbeat":  {"post"},
	"/internal/v1/device-hosts/{id}/heartbeats":              {"post"},
	"/internal/v1/device-hosts/{id}/commands/claims":         {"post"},
	"/internal/v1/device-host-commands/{id}/completions":     {"post"},
	"/internal/v1/device-host-commands/{id}/extensions":      {"post"},
	"/internal/v1/devices/{id}/health-events":                {"post"},
	"/internal/v1/ios-session-fence/grants/consumptions":     {"post"},
	"/internal/v1/ios-session-fence/sessions/bindings":       {"post"},
	"/internal/v1/ios-session-fence/sessions/authorizations": {"post"},
	"/internal/v1/ios-session-fence/sessions/closures":       {"post"},
	"/internal/v1/ios-session-fence/sessions/failures":       {"post"},
}

func TestOpenAPIContract(t *testing.T) {
	document, raw := loadOpenAPI(t)
	if document["openapi"] != "3.0.3" {
		t.Fatalf("openapi = %#v, want 3.0.3", document["openapi"])
	}
	lowerRaw := strings.ToLower(raw)
	if strings.Contains(lowerRaw, "eval-tasks") || strings.Contains(lowerRaw, "eval_tasks") || strings.Contains(lowerRaw, "eval task") {
		t.Fatal("contract must not depend on legacy eval-tasks")
	}
	info := object(t, document, "info")
	if info["version"] != "2.2.0" || info["x-contract-status"] != "frozen" || info["x-platform-semantics"] != "Case/Run/RunAttempt" {
		t.Fatalf("frozen adapter contract metadata=%#v", info)
	}

	paths := object(t, document, "paths")
	if len(paths) != len(expectedOperations) {
		t.Fatalf("path count = %d, want %d", len(paths), len(expectedOperations))
	}
	operationIDs := map[string]bool{}
	assertSecurityOR(t, document, "serviceBearer", "consoleCookie")
	for path, methods := range expectedOperations {
		pathItem := object(t, paths, path)
		for _, method := range methods {
			operation := object(t, pathItem, method)
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" || operationIDs[operationID] {
				t.Fatalf("%s %s has empty or duplicate operationId %#v", method, path, operation["operationId"])
			}
			operationIDs[operationID] = true
			responses := object(t, operation, "responses")
			if len(responses) == 0 {
				t.Fatalf("%s %s has no responses", method, path)
			}
			if strings.HasPrefix(path, "/internal/v1/") {
				assertSecurity(t, operation, "agentBearer")
			} else if strings.HasPrefix(path, "/console/api/v1/") {
				if path == "/console/api/v1/sessions" && method == "post" {
					if security, ok := operation["security"].([]any); !ok || len(security) != 0 {
						t.Fatalf("console login must be public")
					}
				} else {
					assertSecurity(t, operation, "consoleCookie")
				}
			} else if strings.HasPrefix(path, "/api/v1/") {
				for _, status := range []string{"401", "403"} {
					if _, ok := responses[status]; !ok {
						t.Fatalf("%s %s response is missing %s", method, path, status)
					}
				}
			} else if security, ok := operation["security"].([]any); !ok || len(security) != 0 {
				t.Fatalf("public operation %s %s must override security with []", method, path)
			}
		}
	}

	components := object(t, document, "components")
	securitySchemes := object(t, components, "securitySchemes")
	object(t, securitySchemes, "serviceBearer")
	object(t, securitySchemes, "agentBearer")
	object(t, securitySchemes, "consoleCookie")
	validateLocalReferences(t, document, document, "#")
	validateRequestExamples(t, components)
	validateAlcorAdapterContract(t, paths, components)
	validatePlatformNeutralContract(t, components)
	validateConsoleResponseTypes(t, document, paths)
	validateFrozenHash(t, raw)
}

func validatePlatformNeutralContract(t *testing.T, components map[string]any) {
	t.Helper()
	schemas := object(t, components, "schemas")
	checks := []struct {
		schema   string
		property string
		values   string
	}{
		{"DeviceHost", "host_os", "linux,macos,windows"},
		{"DeviceHostInput", "host_os", "linux,macos,windows"},
		{"DevicePool", "platform", "android,ios"},
		{"DevicePoolInput", "platform", "android,ios"},
		{"Device", "platform", "android,ios"},
		{"Device", "device_kind", "emulator,simulator,physical"},
		{"Device", "provider_type", "docker_emulator,usb_android,appium_device_farm_ios,mock"},
	}
	for _, check := range checks {
		schema := object(t, schemas, check.schema)
		property := object(t, object(t, schema, "properties"), check.property)
		if got := strings.Join(stringValues(t, property["enum"]), ","); got != check.values {
			t.Fatalf("%s.%s enum=%s, want %s", check.schema, check.property, got, check.values)
		}
		if !containsString(stringValues(t, schema["required"]), check.property) {
			t.Fatalf("%s must require %s", check.schema, check.property)
		}
	}
}

func validateConsoleResponseTypes(t *testing.T, document, paths map[string]any) {
	t.Helper()
	operations := []struct{ path, method, status string }{
		{"/console/api/v1/sessions", "post", "201"},
		{"/console/api/v1/me", "get", "200"},
		{"/console/api/v1/sessions/current", "delete", "200"},
		{"/console/api/v1/devices/{id}/remote-control", "get", "200"},
		{"/console/api/v1/devices/{id}/remote-control", "post", "202"},
		{"/console/api/v1/devices/{id}/remote-control", "delete", "200"},
		{"/console/api/v1/devices/{id}/remote-control/heartbeat", "post", "200"},
		{"/api/v1/device-images", "get", "200"},
		{"/api/v1/device-hosts", "get", "200"},
		{"/api/v1/device-pools", "get", "200"},
		{"/api/v1/devices", "get", "200"},
		{"/api/v1/device-reservations", "get", "200"},
		{"/api/v1/device-audit-events", "get", "200"},
		{"/api/v1/devices/{id}/health-events", "get", "200"},
	}
	for _, expected := range operations {
		response := object(t, object(t, object(t, paths, expected.path), expected.method), "responses")
		success := object(t, response, expected.status)
		responseValue, ok := resolveReference(document, success["$ref"].(string))
		if !ok {
			t.Fatalf("cannot resolve response for %s %s", expected.method, expected.path)
		}
		responseObject := responseValue.(map[string]any)
		media := object(t, object(t, responseObject, "content"), "application/json")
		schema := object(t, media, "schema")
		if schema["$ref"] == "#/components/schemas/SuccessEnvelope" {
			t.Fatalf("%s %s uses weak SuccessEnvelope", expected.method, expected.path)
		}
	}
}

func validateFrozenHash(t *testing.T, raw string) {
	t.Helper()
	path := filepath.Join("..", "..", "openapi", "device-farm-v1.sha256")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read frozen OpenAPI hash: %v", err)
	}
	canonical := strings.ReplaceAll(raw, "\r\n", "\n")
	actual := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	if expected := strings.TrimSpace(string(content)); actual != expected {
		t.Fatalf("OpenAPI changed without updating frozen hash: actual=%s expected=%s", actual, expected)
	}
}

func validateAlcorAdapterContract(t *testing.T, paths, components map[string]any) {
	t.Helper()
	operations := []struct {
		path, method, successStatus, responseRef string
	}{
		{"/api/v1/device-reservations", "post", "201", "#/components/responses/ReservationCreated"},
		{"/api/v1/device-reservations/{id}", "get", "200", "#/components/responses/ReservationSuccess"},
		{"/api/v1/device-reservations/{id}/extensions", "post", "200", "#/components/responses/ReservationSuccess"},
		{"/api/v1/device-reservations/{id}/releases", "post", "200", "#/components/responses/ReservationSuccess"},
		{"/api/v1/device-reservations/{id}/session-grants", "post", "201", "#/components/responses/IOSSessionGrantCreated"},
		{"/api/v1/devices/{id}", "get", "200", "#/components/responses/DeviceSuccess"},
	}
	for _, expected := range operations {
		operation := object(t, object(t, paths, expected.path), expected.method)
		parameters, ok := operation["parameters"].([]any)
		if !ok {
			t.Fatalf("%s %s parameters=%#v", expected.method, expected.path, operation["parameters"])
		}
		for _, requiredRef := range []string{"#/components/parameters/EvalRunID", "#/components/parameters/EvalAttemptID", "#/components/parameters/Traceparent"} {
			if !containsReference(parameters, requiredRef) {
				t.Fatalf("%s %s is missing %s", expected.method, expected.path, requiredRef)
			}
		}
		response := object(t, object(t, operation, "responses"), expected.successStatus)
		if response["$ref"] != expected.responseRef {
			t.Fatalf("%s %s success response=%#v", expected.method, expected.path, response)
		}
	}

	schemas := object(t, components, "schemas")
	ownerType := object(t, schemas, "OwnerType")
	if strings.Join(stringValues(t, ownerType["enum"]), ",") != "run_attempt,manual,test_run" {
		t.Fatalf("OwnerType enum=%#v", ownerType["enum"])
	}
	apiError := object(t, schemas, "APIError")
	code := object(t, object(t, apiError, "properties"), "code")
	codes := stringValues(t, code["x-adapter-stable-codes"])
	for _, required := range []string{"DEVICE_CAPACITY_UNAVAILABLE", "DEVICE_POOL_UNAVAILABLE", "KVM_UNAVAILABLE",
		"IOS_SESSION_GRANT_EXPIRED", "IOS_SESSION_ROUTING_MISMATCH", "IOS_SESSION_CLEANUP_FAILED", "SERVICE_UNAVAILABLE"} {
		if !containsString(codes, required) {
			t.Fatalf("APIError stable code list is missing %s", required)
		}
	}
	for _, legacy := range []string{"CAPACITY_UNAVAILABLE", "POOL_UNAVAILABLE"} {
		if containsString(codes, legacy) {
			t.Fatalf("APIError stable code list contains legacy code %s", legacy)
		}
	}
}

func containsReference(values []any, reference string) bool {
	for _, value := range values {
		item, ok := value.(map[string]any)
		if ok && item["$ref"] == reference {
			return true
		}
	}
	return false
}

func stringValues(t *testing.T, value any) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("value=%#v, want array", value)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("array value=%#v, want string", value)
		}
		result = append(result, text)
	}
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func loadOpenAPI(t *testing.T) (map[string]any, string) {
	t.Helper()
	path := filepath.Join("..", "..", "openapi", "device-farm-v1.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse OpenAPI YAML: %v", err)
	}
	return document, string(content)
}

func assertSecurity(t *testing.T, operation map[string]any, scheme string) {
	t.Helper()
	security, ok := operation["security"].([]any)
	if !ok || len(security) != 1 {
		t.Fatalf("security = %#v, want one %s requirement", operation["security"], scheme)
	}
	requirement, ok := security[0].(map[string]any)
	if !ok {
		t.Fatalf("security requirement = %#v", security[0])
	}
	if _, ok := requirement[scheme]; !ok {
		t.Fatalf("security requirement = %#v, want %s", requirement, scheme)
	}
}

func assertSecurityOR(t *testing.T, operation map[string]any, schemes ...string) {
	t.Helper()
	security, ok := operation["security"].([]any)
	if !ok || len(security) != len(schemes) {
		t.Fatalf("security = %#v, want OR requirements %v", operation["security"], schemes)
	}
	for _, scheme := range schemes {
		found := false
		for _, item := range security {
			requirement, ok := item.(map[string]any)
			if ok {
				_, found = requirement[scheme]
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("security = %#v, missing %s", security, scheme)
		}
	}
}

func validateLocalReferences(t *testing.T, root map[string]any, value any, location string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				reference, ok := child.(string)
				if !ok || !strings.HasPrefix(reference, "#/") {
					t.Fatalf("%s has unsupported ref %#v", location, child)
				}
				if _, ok := resolveReference(root, reference); !ok {
					t.Fatalf("%s has unresolved ref %q", location, reference)
				}
			}
			validateLocalReferences(t, root, child, location+"/"+key)
		}
	case []any:
		for index, child := range typed {
			validateLocalReferences(t, root, child, fmt.Sprintf("%s/%d", location, index))
		}
	}
}

func resolveReference(root map[string]any, reference string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func validateRequestExamples(t *testing.T, components map[string]any) {
	t.Helper()
	requestBodies := object(t, components, "requestBodies")
	schemas := object(t, components, "schemas")
	for _, name := range []string{"DeviceImageInput", "DeviceHostInput", "DevicePoolInput", "DevicePoolImageInput", "ReservationCreate"} {
		body := object(t, requestBodies, name)
		content := object(t, body, "content")
		media := object(t, content, "application/json")
		example := object(t, media, "example")
		schema := object(t, schemas, name)
		requiredValues, ok := schema["required"].([]any)
		if !ok {
			t.Fatalf("%s.required = %#v", name, schema["required"])
		}
		var missing []string
		for _, value := range requiredValues {
			field := value.(string)
			if _, ok := example[field]; !ok {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Fatalf("%s example missing required fields: %v", name, missing)
		}
	}
}

func object(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key]
	if !ok {
		t.Fatalf("missing object %q", key)
	}
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%q = %T, want object", key, value)
	}
	return result
}
