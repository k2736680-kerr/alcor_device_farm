package alcor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type Config struct {
	BaseURL      string
	Token        string
	HTTPClient   *http.Client
	PollInterval time.Duration
}

type Client struct {
	baseURL      *url.URL
	token        string
	httpClient   *http.Client
	pollInterval time.Duration
}

func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("设备农场地址必须是无凭据、查询参数和片段的 HTTP(S) 地址")
	}
	if strings.TrimSpace(config.Token) == "" {
		return nil, errors.New("必须提供设备农场服务令牌")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	return &Client{baseURL: baseURL, token: config.Token, httpClient: client, pollInterval: pollInterval}, nil
}

func (client *Client) Reserve(ctx context.Context, input ReserveRequest) (Reservation, error) {
	if !identifierPattern.MatchString(input.PoolID) || !validRunContext(input.Run) ||
		input.LeaseSeconds < 60 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return Reservation{}, errors.New("RunAttempt 预约请求无效")
	}
	capabilities := input.RequestedCapabilities
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	payload := map[string]any{
		"pool_id": input.PoolID, "owner_type": OwnerTypeRunAttempt,
		"owner_id": input.Run.RunAttemptID, "requested_capabilities": capabilities,
		"lease_seconds": input.LeaseSeconds,
	}
	if input.RequestedDeviceID != "" {
		if !identifierPattern.MatchString(input.RequestedDeviceID) {
			return Reservation{}, errors.New("RunAttempt 指定设备无效")
		}
		payload["requested_device_id"] = input.RequestedDeviceID
	}
	var result Reservation
	err := client.do(ctx, http.MethodPost, "/api/v1/device-reservations", payload, input.IdempotencyKey, input.Run, &result)
	return result, err
}

func (client *Client) GetReservation(ctx context.Context, id string, run RunContext) (Reservation, error) {
	if !identifierPattern.MatchString(id) || !validRunContext(run) {
		return Reservation{}, errors.New("预约查询参数无效")
	}
	var result Reservation
	err := client.do(ctx, http.MethodGet, "/api/v1/device-reservations/"+url.PathEscape(id), nil, "", run, &result)
	return result, err
}

func (client *Client) WaitActive(ctx context.Context, id string, run RunContext) (Lease, error) {
	ticker := time.NewTicker(client.pollInterval)
	defer ticker.Stop()
	for {
		reservation, err := client.GetReservation(ctx, id, run)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Lease{}, &APIError{Code: CodeDeviceCapacityUnavailable, Message: "预约未能在等待期限内激活", Retryable: true}
			}
			return Lease{}, err
		}
		switch reservation.Status {
		case "active":
			if reservation.DeviceID == nil {
				return Lease{}, errors.New("已激活预约缺少设备 ID")
			}
			device, err := client.getDevice(ctx, *reservation.DeviceID, run)
			if err != nil {
				return Lease{}, err
			}
			return buildLease(reservation, device)
		case "failed", "expired", "released", "force_released":
			code := "INVALID_STATE_TRANSITION"
			if reservation.FailureCode != nil && *reservation.FailureCode != "" {
				code = *reservation.FailureCode
			}
			return Lease{}, &APIError{Code: code, Message: "预约已进入终态：" + reservation.Status}
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Lease{}, &APIError{Code: CodeDeviceCapacityUnavailable, Message: "预约未能在等待期限内激活", Retryable: true}
			}
			return Lease{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (client *Client) Extend(ctx context.Context, id string, additionalSeconds int, idempotencyKey string, run RunContext) (Reservation, error) {
	if !identifierPattern.MatchString(id) || additionalSeconds < 60 || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || !validRunContext(run) {
		return Reservation{}, errors.New("预约续约参数无效")
	}
	var result Reservation
	err := client.do(ctx, http.MethodPost, "/api/v1/device-reservations/"+url.PathEscape(id)+"/extensions",
		map[string]any{"additional_seconds": additionalSeconds}, idempotencyKey, run, &result)
	return result, err
}

func (client *Client) Release(ctx context.Context, id, reason, idempotencyKey string, run RunContext) (Reservation, error) {
	if !identifierPattern.MatchString(id) || len(strings.TrimSpace(reason)) < 3 || len(strings.TrimSpace(reason)) > 500 ||
		len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || !validRunContext(run) {
		return Reservation{}, errors.New("预约释放参数无效")
	}
	var result Reservation
	err := client.do(ctx, http.MethodPost, "/api/v1/device-reservations/"+url.PathEscape(id)+"/releases",
		map[string]any{"reason": strings.TrimSpace(reason)}, idempotencyKey, run, &result)
	return result, err
}

func (client *Client) getDevice(ctx context.Context, id string, run RunContext) (Device, error) {
	var result Device
	err := client.do(ctx, http.MethodGet, "/api/v1/devices/"+url.PathEscape(id), nil, "", run, &result)
	return result, err
}

func (client *Client) do(ctx context.Context, method, path string, payload any, idempotencyKey string, run RunContext, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("编码设备农场请求失败：%w", err)
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := *client.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("创建设备农场请求失败：%w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Eval-Run-Id", run.RunID)
	request.Header.Set("X-Eval-Attempt-Id", run.RunAttemptID)
	if run.Traceparent != "" {
		request.Header.Set("traceparent", run.Traceparent)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("调用设备农场失败：%w", err)
	}
	defer response.Body.Close()
	var envelope struct {
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
		Error     *APIError       `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("解析设备农场响应失败：%w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Error != nil {
		apiError := envelope.Error
		if apiError == nil {
			apiError = &APIError{Code: "INTERNAL_ERROR", Message: "设备农场返回了失败响应"}
		}
		apiError.HTTPStatus = response.StatusCode
		apiError.RequestID = envelope.RequestID
		return apiError
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("解析设备农场数据失败：%w", err)
	}
	return nil
}

func validRunContext(run RunContext) bool {
	return identifierPattern.MatchString(run.RunID) && identifierPattern.MatchString(run.RunAttemptID) &&
		len(run.Traceparent) <= 128 && !strings.ContainsAny(run.Traceparent, "\r\n")
}

func buildLease(reservation Reservation, device Device) (Lease, error) {
	if device.Serial == "" || device.AppiumEndpoint == nil || *device.AppiumEndpoint == "" {
		return Lease{}, errors.New("已激活设备缺少序列号或 Appium 端点")
	}
	appiumUDID := device.Serial
	for _, key := range []string{"appiumUdid", "appium_udid"} {
		if value, ok := device.Capabilities[key].(string); ok && value != "" {
			appiumUDID = value
			break
		}
	}
	adbEndpoint := ""
	if device.ADBEndpoint != nil {
		adbEndpoint = *device.ADBEndpoint
	}
	return Lease{Reservation: reservation, Device: device, AppiumUDID: appiumUDID,
		ADBEndpoint: adbEndpoint, AppiumEndpoint: *device.AppiumEndpoint}, nil
}
