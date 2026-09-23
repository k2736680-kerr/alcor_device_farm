package iossession

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxSessionRequestBytes = 1 << 20

func validateAndPinSessionRequest(raw json.RawMessage, expectedUDID string) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxSessionRequestBytes || strings.TrimSpace(expectedUDID) == "" {
		return nil, ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%w: invalid W3C new Session payload", ErrInvalidArgument)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: only one JSON document is accepted", ErrInvalidArgument)
	}
	if len(envelope) != 1 {
		return nil, fmt.Errorf("%w: only W3C capabilities are accepted", ErrInvalidArgument)
	}
	capabilities, ok := envelope["capabilities"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: W3C capabilities are required", ErrInvalidArgument)
	}
	alwaysMatch, ok := capabilities["alwaysMatch"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: capabilities.alwaysMatch is required", ErrInvalidArgument)
	}
	firstMatch, exists := capabilities["firstMatch"]
	if !exists {
		capabilities["firstMatch"] = []any{map[string]any{}}
	} else {
		candidates, ok := firstMatch.([]any)
		if !ok || len(candidates) != 1 {
			return nil, fmt.Errorf("%w: exactly one firstMatch candidate is allowed", ErrInvalidArgument)
		}
		candidate, ok := candidates[0].(map[string]any)
		if !ok || hasRoutingCapability(candidate) {
			return nil, fmt.Errorf("%w: routing capabilities are not allowed in firstMatch", ErrInvalidArgument)
		}
		if forbiddenSelector(candidate) != "" {
			return nil, fmt.Errorf("%w: Device Farm selectors are not allowed", ErrInvalidArgument)
		}
	}
	if selector := forbiddenSelector(alwaysMatch); selector != "" {
		return nil, fmt.Errorf("%w: capability %s is not allowed", ErrInvalidArgument, selector)
	}
	platform, ok := alwaysMatch["platformName"].(string)
	if !ok || !strings.EqualFold(strings.TrimSpace(platform), "ios") {
		return nil, fmt.Errorf("%w: platformName must be iOS", ErrInvalidArgument)
	}
	udid, ok := alwaysMatch["appium:udid"].(string)
	if !ok || strings.TrimSpace(udid) != expectedUDID {
		return nil, fmt.Errorf("%w: appium:udid does not match the Reservation", ErrRoutingMismatch)
	}
	dfUDID, ok := alwaysMatch["df:udids"].(string)
	if !ok || strings.TrimSpace(dfUDID) != expectedUDID {
		return nil, fmt.Errorf("%w: df:udids does not match the Reservation", ErrRoutingMismatch)
	}
	if automation, exists := alwaysMatch["appium:automationName"]; exists {
		value, ok := automation.(string)
		if !ok || !strings.EqualFold(strings.TrimSpace(value), "xcuitest") {
			return nil, fmt.Errorf("%w: appium:automationName must be XCUITest", ErrInvalidArgument)
		}
	}
	alwaysMatch["platformName"] = "iOS"
	alwaysMatch["appium:udid"] = expectedUDID
	alwaysMatch["df:udids"] = expectedUDID
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("%w: encode pinned Session request", ErrInvalidArgument)
	}
	return encoded, nil
}

func forbiddenSelector(capabilities map[string]any) string {
	for key := range capabilities {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
		switch normalized {
		case "tags", "df:tags", "filterbyhost", "df:filterbyhost":
			return key
		}
	}
	return ""
}

func hasRoutingCapability(capabilities map[string]any) bool {
	for key := range capabilities {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "platformname", "appium:udid", "df:udids":
			return true
		}
	}
	return false
}
