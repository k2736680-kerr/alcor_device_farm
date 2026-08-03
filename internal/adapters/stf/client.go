package stf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Device struct {
	Serial  string `json:"serial"`
	Present bool   `json:"present"`
	Ready   bool   `json:"ready"`
	Using   bool   `json:"using"`
}

type RemoteConnection struct {
	URL string
}

type Error struct {
	Code       string
	Message    string
	Retryable  bool
	StatusCode int
	Cause      error
}

func (err *Error) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("STF %s: %s: %v", err.Code, err.Message, err.Cause)
	}
	return fmt.Sprintf("STF %s: %s", err.Code, err.Message)
}

func (err *Error) Unwrap() error     { return err.Cause }
func (err *Error) IsRetryable() bool { return err.Retryable }

type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	Attempts   int
	RetryDelay time.Duration
	HTTPClient *http.Client
}

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	attempts   int
	retryDelay time.Duration
}

func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("STF base URL must be an absolute HTTP(S) URL without user info, query or fragment")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("STF base URL must use HTTP or HTTPS")
	}
	if strings.TrimSpace(config.Token) == "" {
		return nil, errors.New("STF API token is required")
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.Attempts <= 0 {
		config.Attempts = 3
	}
	if config.Attempts > 5 {
		return nil, errors.New("STF attempts must not exceed 5")
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 200 * time.Millisecond
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.Timeout}
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return &Client{baseURL: baseURL, token: config.Token, httpClient: config.HTTPClient,
		attempts: config.Attempts, retryDelay: config.RetryDelay}, nil
}

func (client *Client) Inventory(ctx context.Context) ([]Device, error) {
	var response struct {
		Devices []Device `json:"devices"`
	}
	if err := client.request(ctx, http.MethodGet, "/api/v1/devices?fields=serial,present,ready,using", nil, "STF_INVENTORY_FAILED", &response); err != nil {
		return nil, err
	}
	return response.Devices, nil
}

func (client *Client) Health(ctx context.Context) error {
	_, err := client.Inventory(ctx)
	return err
}

func (client *Client) Visible(ctx context.Context, serial string) (bool, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return false, &Error{Code: "INVALID_ARGUMENT", Message: "serial is required"}
	}
	devices, err := client.Inventory(ctx)
	if err != nil {
		return false, err
	}
	for _, device := range devices {
		if device.Serial == serial {
			return device.Present, nil
		}
	}
	return false, nil
}

func (client *Client) Claim(ctx context.Context, serial string, ttl time.Duration) error {
	serial = strings.TrimSpace(serial)
	if serial == "" || ttl < 30*time.Second || ttl > 24*time.Hour {
		return &Error{Code: "INVALID_ARGUMENT", Message: "serial and a valid STF claim TTL are required"}
	}
	payload := map[string]any{"serial": serial, "timeout": ttl.Milliseconds()}
	var response operationResponse
	if err := client.request(ctx, http.MethodPost, "/api/v1/user/devices", payload, "STF_CLAIM_FAILED", &response); err != nil {
		return err
	}
	if !response.Success {
		return &Error{Code: "STF_CLAIM_FAILED", Message: "STF did not claim the device", Retryable: true}
	}
	return nil
}

func (client *Client) Release(ctx context.Context, serial string) error {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return &Error{Code: "INVALID_ARGUMENT", Message: "serial is required"}
	}
	var response operationResponse
	if err := client.request(ctx, http.MethodDelete, "/api/v1/user/devices/"+url.PathEscape(serial), nil, "STF_RELEASE_FAILED", &response); err != nil {
		return err
	}
	if !response.Success {
		return &Error{Code: "STF_RELEASE_FAILED", Message: "STF did not release the device", Retryable: true}
	}
	return nil
}

func (client *Client) RemoteConnect(ctx context.Context, serial string) (RemoteConnection, error) {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return RemoteConnection{}, &Error{Code: "INVALID_ARGUMENT", Message: "serial is required"}
	}
	var response struct {
		Success          bool   `json:"success"`
		RemoteConnectURL string `json:"remoteConnectUrl"`
	}
	if err := client.request(ctx, http.MethodPost, "/api/v1/user/devices/"+url.PathEscape(serial)+"/remoteConnect", nil, "STF_REMOTE_CONNECT_FAILED", &response); err != nil {
		return RemoteConnection{}, err
	}
	if !response.Success || strings.TrimSpace(response.RemoteConnectURL) == "" {
		return RemoteConnection{}, &Error{Code: "STF_BAD_RESPONSE", Message: "remoteConnect response did not contain a URL"}
	}
	remoteURL, err := safeRemoteConnectURL(response.RemoteConnectURL)
	if err != nil {
		return RemoteConnection{}, &Error{Code: "STF_BAD_RESPONSE", Message: "remoteConnect response contained an unsafe URL", Cause: err}
	}
	return RemoteConnection{URL: remoteURL}, nil
}

func safeRemoteConnectURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "?#@\r\n") {
		return "", errors.New("remoteConnect URL must not contain credentials, query, fragment or control characters")
	}
	address := value
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "tcp" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
			return "", errors.New("remoteConnect URL must use tcp://host:port or host:port")
		}
		address = parsed.Host
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return "", errors.New("remoteConnect URL must contain host and port")
	}
	return value, nil
}

func (client *Client) RemoteDisconnect(ctx context.Context, serial string) error {
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return &Error{Code: "INVALID_ARGUMENT", Message: "serial is required"}
	}
	var response operationResponse
	if err := client.request(ctx, http.MethodDelete, "/api/v1/user/devices/"+url.PathEscape(serial)+"/remoteConnect", nil, "STF_REMOTE_DISCONNECT_FAILED", &response); err != nil {
		return err
	}
	if !response.Success {
		return &Error{Code: "STF_REMOTE_DISCONNECT_FAILED", Message: "STF did not close remoteConnect", Retryable: true}
	}
	return nil
}

type operationResponse struct {
	Success bool `json:"success"`
}

func (client *Client) request(ctx context.Context, method, path string, payload any, code string, target any) error {
	var encoded []byte
	var err error
	if payload != nil {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	for attempt := 1; attempt <= client.attempts; attempt++ {
		response, requestErr := client.do(ctx, method, path, encoded)
		if requestErr == nil {
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				if target == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					response.Body.Close()
					return nil
				}
				decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
				if err := decoder.Decode(target); err != nil {
					response.Body.Close()
					return &Error{Code: "STF_BAD_RESPONSE", Message: "STF response was not valid JSON", Cause: err}
				}
				response.Body.Close()
				return nil
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
			if method == http.MethodDelete && response.StatusCode == http.StatusNotFound {
				if operation, ok := target.(*operationResponse); ok {
					operation.Success = true
				}
				return nil
			}
			requestErr = &Error{Code: code, Message: "STF request was rejected", Retryable: retryable, StatusCode: response.StatusCode}
		}
		var typed *Error
		if !errors.As(requestErr, &typed) {
			typed = &Error{Code: code, Message: "STF request failed", Retryable: true, Cause: requestErr}
			requestErr = typed
		}
		if !typed.Retryable || attempt == client.attempts {
			return requestErr
		}
		timer := time.NewTimer(client.retryDelay * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return &Error{Code: code, Message: "STF request context ended", Retryable: true, Cause: ctx.Err()}
		case <-timer.C:
		}
	}
	return &Error{Code: code, Message: "STF request failed", Retryable: true}
}

func (client *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	target := *client.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/" + strings.TrimLeft(reference.Path, "/")
	target.RawQuery = reference.RawQuery
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return client.httpClient.Do(request)
}
