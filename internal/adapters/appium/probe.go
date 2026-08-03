package appium

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

const maxStatusResponseBytes = 1 << 20

type Probe struct {
	client *http.Client
}

func NewProbe(timeout time.Duration) (*Probe, error) {
	if timeout <= 0 {
		return nil, errors.New("Appium health timeout must be positive")
	}
	return &Probe{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("Appium health redirects are not allowed")
		},
	}}, nil
}

func (probe *Probe) Healthy(ctx context.Context, connection providers.ConnectionInfo) (bool, error) {
	if probe == nil || probe.client == nil {
		return false, errors.New("Appium health probe is not configured")
	}
	statusURL, err := statusEndpoint(connection.AppiumEndpoint)
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := probe.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("request Appium status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return false, fmt.Errorf("Appium status returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Value struct {
			Ready *bool `json:"ready"`
		} `json:"value"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxStatusResponseBytes))
	if err := decoder.Decode(&payload); err != nil {
		return false, fmt.Errorf("decode Appium status: %w", err)
	}
	if payload.Value.Ready == nil {
		return false, errors.New("Appium status response does not contain value.ready")
	}
	return *payload.Value.Ready, nil
}

func statusEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Appium endpoint")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/status"
	return parsed.String(), nil
}

var _ interface {
	Healthy(context.Context, providers.ConnectionInfo) (bool, error)
} = (*Probe)(nil)
