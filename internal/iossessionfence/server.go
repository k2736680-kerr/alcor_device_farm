package iossessionfence

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/iossession"
)

const (
	maxCreateBody  = 1 << 20
	maxCommandBody = 16 << 20
	maxCreateReply = 4 << 20
)

type Config struct {
	ListenAddress    string
	AdvertiseURL     string
	ControlServerURL string
	AgentToken       string
	HostID           string
	UpstreamEndpoint string
	Timeout          time.Duration
	ShutdownTimeout  time.Duration
	Logger           *slog.Logger
	HTTPClient       *http.Client
}

type Server struct {
	config         Config
	controlURL     *url.URL
	upstreamURL    *url.URL
	httpClient     *http.Client
	streamClient   *http.Client
	httpServer     *http.Server
	remoteSessions sync.Map
}

func New(config Config) (*Server, error) {
	config.ListenAddress = strings.TrimSpace(config.ListenAddress)
	config.AdvertiseURL = strings.TrimSpace(config.AdvertiseURL)
	config.ControlServerURL = strings.TrimSpace(config.ControlServerURL)
	config.AgentToken = strings.TrimSpace(config.AgentToken)
	config.HostID = strings.TrimSpace(config.HostID)
	if config.ListenAddress == "" || config.AdvertiseURL == "" || len(config.AgentToken) < 16 || !validIdentifier(config.HostID) {
		return nil, errors.New("invalid iOS Session Fence configuration")
	}
	if _, err := validateAdvertiseURL(config.AdvertiseURL); err != nil {
		return nil, err
	}
	controlURL, err := parseEndpoint(config.ControlServerURL, false)
	if err != nil {
		return nil, fmt.Errorf("invalid Session Fence control Server URL: %w", err)
	}
	upstreamURL, err := parseEndpoint(config.UpstreamEndpoint, true)
	if err != nil {
		return nil, fmt.Errorf("invalid Session Fence Appium endpoint: %w", err)
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 10 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	} else {
		copy := *client
		client = &copy
	}
	client.Timeout = config.Timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("iOS Session Fence redirects are not allowed")
	}
	streamClient := &http.Client{CheckRedirect: client.CheckRedirect}
	server := &Server{config: config, controlURL: controlURL, upstreamURL: upstreamURL, httpClient: client, streamClient: streamClient}
	server.httpServer = &http.Server{Addr: config.ListenAddress, Handler: server, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: config.Timeout, IdleTimeout: 60 * time.Second}
	return server, nil
}

func (server *Server) AdvertiseURL() string {
	return strings.TrimRight(server.config.AdvertiseURL, "/")
}

func (server *Server) Run(ctx context.Context) error {
	errorChannel := make(chan error, 1)
	go func() {
		err := server.httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorChannel <- err
	}()
	select {
	case err := <-errorChannel:
		return err
	case <-ctx.Done():
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), server.config.ShutdownTimeout)
	defer cancel()
	if err := server.httpServer.Shutdown(shutdownContext); err != nil {
		return err
	}
	return <-errorChannel
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	if strings.HasPrefix(request.URL.Path, "/internal/v1/ios-remote/sessions/") {
		server.remote(writer, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/internal/v1/ios-session-fence/sessions/") {
		server.cleanup(writer, request)
		return
	}
	grant, ok := sessionGrant(request.Header.Get("Authorization"))
	if !ok {
		http.Error(writer, "需要有效的 Session Grant", http.StatusUnauthorized)
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/session" {
		server.create(writer, request, grant)
		return
	}
	sessionID, ok := boundSessionPath(request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	server.proxyBound(writer, request, grant, sessionID)
}

func (server *Server) create(writer http.ResponseWriter, request *http.Request, grant string) {
	raw, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxCreateBody))
	if err != nil {
		http.Error(writer, "Session 请求无效", http.StatusBadRequest)
		return
	}
	var authorization iossession.ConsumeView
	err = server.control(request.Context(), "/internal/v1/ios-session-fence/grants/consumptions", iossession.ConsumeInput{
		HostID: server.config.HostID, SessionGrant: grant, Request: raw,
	}, &authorization)
	if err != nil {
		writeControlError(writer, err)
		return
	}
	if !sameEndpoint(authorization.UpstreamEndpoint, server.upstreamURL.String()) {
		server.recordFailure(request.Context(), grant, "IOS_UPSTREAM_ENDPOINT_MISMATCH")
		http.Error(writer, "Session 路由校验未通过", http.StatusConflict)
		return
	}
	upstreamRequest, mjpegPort, err := prepareMJPEGCapabilities(authorization.Request)
	if err != nil {
		server.recordFailure(request.Context(), grant, "IOS_REMOTE_PORT_ALLOCATION_FAILED")
		http.Error(writer, "无法为 iOS 画面分配端口", http.StatusServiceUnavailable)
		return
	}
	response, body, err := server.callUpstream(request.Context(), http.MethodPost, "/session", upstreamRequest, request.Header.Get("Content-Type"), maxCreateReply)
	if err != nil {
		server.recordFailure(request.Context(), grant, "APPIUM_SESSION_CREATE_FAILED")
		http.Error(writer, "上游 Appium Session 当前不可用", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		server.recordFailure(request.Context(), grant, classifySessionCreateFailure(body))
		copyResponse(writer, response, body)
		return
	}
	sessionID := appiumSessionID(body)
	if sessionID == "" {
		server.recordFailure(request.Context(), grant, "APPIUM_SESSION_RESPONSE_INVALID")
		http.Error(writer, "上游 Appium 响应未包含 Session ID", http.StatusBadGateway)
		return
	}
	if err := server.control(request.Context(), "/internal/v1/ios-session-fence/sessions/bindings", iossession.BindingInput{
		HostID: server.config.HostID, SessionGrant: grant, AppiumSessionID: sessionID,
	}, nil); err != nil {
		_ = server.deleteUpstream(context.Background(), sessionID)
		server.recordFailure(context.Background(), grant, "IOS_SESSION_BIND_FAILED")
		writeControlError(writer, err)
		return
	}
	server.remoteSessions.Store(sessionID, mjpegPort)
	copyResponse(writer, response, body)
}

func (server *Server) proxyBound(writer http.ResponseWriter, request *http.Request, grant, sessionID string) {
	var authorization iossession.AuthorizationView
	if err := server.control(request.Context(), "/internal/v1/ios-session-fence/sessions/authorizations", iossession.AuthorizationInput{
		HostID: server.config.HostID, SessionGrant: grant, AppiumSessionID: sessionID,
	}, &authorization); err != nil {
		writeControlError(writer, err)
		return
	}
	if !sameEndpoint(authorization.UpstreamEndpoint, server.upstreamURL.String()) {
		http.Error(writer, "Session routing was rejected", http.StatusConflict)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxCommandBody))
	if err != nil {
		http.Error(writer, "WebDriver request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	upstreamPath := request.URL.Path
	if request.URL.RawQuery != "" {
		upstreamPath += "?" + request.URL.RawQuery
	}
	response, err := server.streamUpstream(request.Context(), request.Method, upstreamPath, body, request.Header.Get("Content-Type"))
	if err != nil {
		http.Error(writer, "upstream Appium request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	copySafeHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
	if request.Method == http.MethodDelete && request.URL.Path == "/session/"+sessionID &&
		(response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300) {
		if err := server.control(context.WithoutCancel(request.Context()), "/internal/v1/ios-session-fence/sessions/closures", iossession.BindingInput{
			HostID: server.config.HostID, SessionGrant: grant, AppiumSessionID: sessionID,
		}, nil); err != nil {
			server.config.Logger.Error("record closed iOS Appium Session failed", "error", err)
		}
	}
}

func (server *Server) cleanup(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodDelete || !constantBearer(request.Header.Get("Authorization"), server.config.AgentToken) ||
		request.Header.Get("X-Device-Farm-Host-Id") != server.config.HostID {
		http.Error(writer, "cleanup request is not authorized", http.StatusForbidden)
		return
	}
	sessionID := strings.TrimPrefix(request.URL.Path, "/internal/v1/ios-session-fence/sessions/")
	if !validAppiumSessionID(sessionID) || strings.Contains(sessionID, "/") {
		http.Error(writer, "invalid Appium Session ID", http.StatusBadRequest)
		return
	}
	if err := server.deleteUpstream(request.Context(), sessionID); err != nil {
		http.Error(writer, "upstream Appium cleanup failed", http.StatusBadGateway)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) deleteUpstream(ctx context.Context, sessionID string) error {
	response, err := server.streamUpstream(ctx, http.MethodDelete, "/session/"+url.PathEscape(sessionID), nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode == http.StatusNotFound || response.StatusCode >= 200 && response.StatusCode < 300 {
		server.remoteSessions.Delete(sessionID)
		return nil
	}
	return fmt.Errorf("upstream cleanup status %d", response.StatusCode)
}

func (server *Server) control(ctx context.Context, path string, input, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return err
	}
	endpoint := *server.controlURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+server.config.AgentToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return errors.New("invalid Session Fence control response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Error != nil {
		code := "IOS_SESSION_CONTROL_FAILED"
		retryable := false
		if envelope.Error != nil {
			code, retryable = envelope.Error.Code, envelope.Error.Retryable
		}
		return &ControlError{Status: response.StatusCode, Code: code, Retryable: retryable}
	}
	if output != nil && json.Unmarshal(envelope.Data, output) != nil {
		return errors.New("invalid Session Fence control data")
	}
	return nil
}

type ControlError struct {
	Status    int
	Code      string
	Retryable bool
}

func (err *ControlError) Error() string { return err.Code }

func writeControlError(writer http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var control *ControlError
	if errors.As(err, &control) && control.Status >= 400 && control.Status <= 599 {
		status = control.Status
	}
	http.Error(writer, "Session authorization failed", status)
}

func (server *Server) callUpstream(ctx context.Context, method, path string, body []byte, contentType string, limit int64) (*http.Response, []byte, error) {
	response, err := server.streamUpstream(ctx, method, path, body, contentType)
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		response.Body.Close()
		return nil, nil, errors.New("upstream Appium response is too large")
	}
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, raw, nil
}

func (server *Server) streamUpstream(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	endpoint := *server.upstreamURL
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		endpoint.Path, endpoint.RawQuery = parts[0], parts[1]
	} else {
		endpoint.Path = path
		endpoint.RawQuery = ""
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", "application/json")
	return server.httpClient.Do(request)
}

func copyResponse(writer http.ResponseWriter, response *http.Response, body []byte) {
	copySafeHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(body)
}

func copySafeHeaders(target, source http.Header) {
	for _, name := range []string{"Content-Type", "Cache-Control", "Pragma"} {
		if value := source.Get(name); value != "" {
			target.Set(name, value)
		}
	}
}

func appiumSessionID(body []byte) string {
	var response struct {
		SessionID string `json:"sessionId"`
		Value     struct {
			SessionID string `json:"sessionId"`
		} `json:"value"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	if response.Value.SessionID != "" {
		response.SessionID = response.Value.SessionID
	}
	if !validAppiumSessionID(response.SessionID) {
		return ""
	}
	return response.SessionID
}

func classifySessionCreateFailure(body []byte) string {
	message := strings.ToLower(string(body))
	switch {
	case strings.Contains(message, "developer mode"):
		return "IOS_DEVELOPER_MODE_DISABLED"
	case strings.Contains(message, "not trusted"), strings.Contains(message, "trust this computer"),
		strings.Contains(message, "pair record"), strings.Contains(message, "not paired"):
		return "IOS_PHYSICAL_NOT_TRUSTED"
	case strings.Contains(message, "device support files"), strings.Contains(message, "developer disk image"),
		strings.Contains(message, "unsupported os version"), strings.Contains(message, "xcode version"):
		return "IOS_XCODE_INCOMPATIBLE"
	case strings.Contains(message, "provisioning profile"), strings.Contains(message, "code signing"),
		strings.Contains(message, "codesign"), strings.Contains(message, "development team"),
		strings.Contains(message, "invalid code signature"), strings.Contains(message, "xcodebuild failed with code 65"):
		return "WDA_SIGNING_FAILED"
	case strings.Contains(message, "webdriveragent"), strings.Contains(message, "wda"):
		return "WDA_START_FAILED"
	default:
		return "APPIUM_SESSION_CREATE_REJECTED"
	}
}

func (server *Server) recordFailure(ctx context.Context, grant, code string) {
	failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := server.control(failureContext, "/internal/v1/ios-session-fence/sessions/failures", iossession.FailureInput{
		HostID: server.config.HostID, SessionGrant: grant, ErrorCode: code,
	}, nil); err != nil {
		server.config.Logger.Error("record iOS Session Fence failure failed", "error", err)
	}
}

func sessionGrant(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Session-Grant") || !validGrant(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func constantBearer(header, expected string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) != len(expected) || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expected)) == 1
}

func boundSessionPath(path string) (string, bool) {
	if !strings.HasPrefix(path, "/session/") {
		return "", false
	}
	value := strings.TrimPrefix(path, "/session/")
	if index := strings.IndexByte(value, '/'); index >= 0 {
		value = value[:index]
	}
	decoded, err := url.PathUnescape(value)
	return decoded, err == nil && validAppiumSessionID(decoded)
}

func validAppiumSessionID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && !strings.ContainsRune("._:-", character) {
			return false
		}
	}
	return true
}

func validGrant(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func validIdentifier(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func parseEndpoint(raw string, loopbackOnly bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid endpoint")
	}
	if loopbackOnly {
		hostname := strings.ToLower(parsed.Hostname())
		ip := net.ParseIP(hostname)
		if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("upstream Appium must use loopback")
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func validateAdvertiseURL(raw string) (string, error) {
	parsed, err := parseEndpoint(raw, false)
	if err != nil {
		return "", err
	}
	hostname := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(hostname)
	loopback := hostname == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme != "https" && !loopback {
		return "", errors.New("non-loopback Fence advertise URL requires HTTPS")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func sameEndpoint(left, right string) bool {
	leftURL, leftErr := parseEndpoint(left, true)
	rightURL, rightErr := parseEndpoint(right, true)
	return leftErr == nil && rightErr == nil && leftURL.String() == rightURL.String()
}
