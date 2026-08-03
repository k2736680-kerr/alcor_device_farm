package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var expectedOperations = map[string][]string{
	"/healthz":                                           {"get"},
	"/readyz":                                            {"get"},
	"/api/v1/device-images":                              {"get", "post"},
	"/api/v1/device-images/{id}":                         {"get", "put"},
	"/api/v1/device-images/{id}/validations":             {"post"},
	"/api/v1/device-hosts":                               {"get", "post"},
	"/api/v1/device-hosts/{id}":                          {"get", "put"},
	"/api/v1/device-hosts/{id}/drains":                   {"post", "delete"},
	"/api/v1/device-pools":                               {"get", "post"},
	"/api/v1/device-pools/{id}":                          {"get", "put"},
	"/api/v1/device-pools/{id}/devices":                  {"post", "delete"},
	"/api/v1/devices":                                    {"get"},
	"/api/v1/devices/{id}":                               {"get"},
	"/api/v1/devices/{id}/restarts":                      {"post"},
	"/api/v1/devices/{id}/rebuilds":                      {"post"},
	"/api/v1/devices/{id}/quarantines":                   {"post", "delete"},
	"/api/v1/device-reservations":                        {"get", "post"},
	"/api/v1/device-reservations/{id}":                   {"get"},
	"/api/v1/device-reservations/{id}/extensions":        {"post"},
	"/api/v1/device-reservations/{id}/releases":          {"post"},
	"/api/v1/device-reservations/{id}/remote-sessions":   {"post"},
	"/internal/v1/device-hosts/{id}/heartbeats":          {"post"},
	"/internal/v1/device-hosts/{id}/commands/claims":     {"post"},
	"/internal/v1/device-host-commands/{id}/completions": {"post"},
	"/internal/v1/devices/{id}/health-events":            {"post"},
}

func TestOpenAPIContract(t *testing.T) {
	document, raw := loadOpenAPI(t)
	if document["openapi"] != "3.0.3" {
		t.Fatalf("openapi = %#v, want 3.0.3", document["openapi"])
	}
	if strings.Contains(strings.ToLower(raw), "eval-tasks") || strings.Contains(strings.ToLower(raw), "eval_tasks") {
		t.Fatal("contract must not depend on legacy eval-tasks")
	}

	paths := object(t, document, "paths")
	if len(paths) != len(expectedOperations) {
		t.Fatalf("path count = %d, want %d", len(paths), len(expectedOperations))
	}
	operationIDs := map[string]bool{}
	assertSecurity(t, document, "serviceBearer")
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
	validateLocalReferences(t, document, document, "#")
	validateRequestExamples(t, components)
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
	for _, name := range []string{"DeviceImageInput", "DeviceHostInput", "DevicePoolInput", "ReservationCreate"} {
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
