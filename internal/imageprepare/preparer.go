// Package imageprepare executes the Build Agent's locally configured script.
// The Server never supplies a URL, executable path, or shell fragment.
package imageprepare

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Preparer interface {
	SyncCatalog(context.Context) (map[string]any, error)
	Prepare(context.Context, string, string) (map[string]any, error)
}

type Script struct{ path string }

func New(path string) (*Script, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("image preparation script is required")
	}
	return &Script{path: path}, nil
}

func (script *Script) SyncCatalog(ctx context.Context) (map[string]any, error) {
	return script.run(ctx, "catalog")
}
func (script *Script) Prepare(ctx context.Context, packageName, revision string) (map[string]any, error) {
	parts := strings.Split(packageName, ";")
	if len(parts) != 4 || parts[0] != "system-images" || !strings.HasPrefix(parts[1], "android-") || (parts[2] != "default" && parts[2] != "google_apis" && parts[2] != "google_play") || (parts[3] != "x86_64" && parts[3] != "arm64-v8a") {
		return nil, errors.New("invalid Android SDK package")
	}
	if !validRevision(revision) {
		return nil, errors.New("invalid Android SDK package revision")
	}
	return script.run(ctx, "prepare", packageName, revision)
}

func validRevision(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func (script *Script) run(ctx context.Context, arguments ...string) (map[string]any, error) {
	command := exec.CommandContext(ctx, script.path, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("image preparation script: %w", err)
	}
	return decodeResult(output)
}

func decodeResult(output []byte) (map[string]any, error) {
	const prefix = "DEVICE_FARM_IMAGE_RESULT="
	var raw string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); strings.HasPrefix(line, prefix) {
			raw = strings.TrimPrefix(line, prefix)
		}
	}
	if raw == "" {
		return nil, errors.New("image preparation script did not return a result")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("decode image preparation result: %w", err)
	}
	return result, nil
}
