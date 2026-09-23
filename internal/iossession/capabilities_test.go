package iossession

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateAndPinSessionRequest(t *testing.T) {
	raw := json.RawMessage(`{"capabilities":{"alwaysMatch":{"platformName":"ios","appium:automationName":"XCUITest","appium:udid":"SIM-1","df:udids":"SIM-1","appium:noReset":true},"firstMatch":[{}]}}`)
	result, err := validateAndPinSessionRequest(raw, "SIM-1")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(result, &decoded) != nil {
		t.Fatalf("invalid result: %s", result)
	}
}

func TestValidateAndPinSessionRequestRejectsDeviceFarmSelection(t *testing.T) {
	tests := []string{
		`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-2","df:udids":"SIM-1"},"firstMatch":[{}]}}`,
		`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-1","df:udids":"SIM-1,SIM-2"},"firstMatch":[{}]}}`,
		`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-1","df:udids":"SIM-1","df:tags":["free"]},"firstMatch":[{}]}}`,
		`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-1","df:udids":"SIM-1"},"firstMatch":[{},{}]}}`,
		`{"desiredCapabilities":{"platformName":"iOS","appium:udid":"SIM-1"}}`,
		`{"capabilities":{"alwaysMatch":{"platformName":"iOS","appium:udid":"SIM-1","df:udids":"SIM-1"},"firstMatch":[{}]}} {}`,
	}
	for _, raw := range tests {
		if _, err := validateAndPinSessionRequest(json.RawMessage(raw), "SIM-1"); err == nil {
			t.Fatalf("request must be rejected: %s", raw)
		}
	}
	if _, err := validateAndPinSessionRequest(json.RawMessage(tests[0]), "SIM-1"); !errors.Is(err, ErrRoutingMismatch) {
		t.Fatalf("mismatch error=%v", err)
	}
}
