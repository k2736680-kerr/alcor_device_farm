package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
)

type HTTPClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHTTPClient(baseURL, token string, client *http.Client) (*HTTPClient, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" {
		return nil, errors.New("agent server URL and token are required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: client}, nil
}

func (client *HTTPClient) Heartbeat(ctx context.Context, hostID string, input hostcommand.HeartbeatInput) error {
	return client.call(ctx, http.MethodPost, "/internal/v1/device-hosts/"+hostID+"/heartbeats", input, nil)
}

func (client *HTTPClient) Claim(ctx context.Context, hostID string, input hostcommand.ClaimInput) ([]hostcommand.Command, error) {
	var response struct {
		Items []hostcommand.Command `json:"items"`
	}
	err := client.call(ctx, http.MethodPost, "/internal/v1/device-hosts/"+hostID+"/commands/claims", input, &response)
	return response.Items, err
}

func (client *HTTPClient) Complete(ctx context.Context, commandID string, input hostcommand.CompletionInput) error {
	return client.call(ctx, http.MethodPost, "/internal/v1/device-host-commands/"+commandID+"/completions", input, nil)
}

func (client *HTTPClient) call(ctx context.Context, method, path string, input, output any) error {
	content, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, bytes.NewReader(content))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		Data  json.RawMessage                 `json:"data"`
		Error *struct{ Code, Message string } `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if envelope.Error != nil {
			return fmt.Errorf("agent API %s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return fmt.Errorf("agent API status %d", response.StatusCode)
	}
	if output != nil {
		return json.Unmarshal(envelope.Data, output)
	}
	return nil
}
