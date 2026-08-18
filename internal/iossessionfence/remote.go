package iossessionfence

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxRemoteActionBody = 16 << 10

type remoteAction struct {
	Type     string  `json:"type"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	EndX     float64 `json:"end_x,omitempty"`
	EndY     float64 `json:"end_y,omitempty"`
	Duration int     `json:"duration_ms,omitempty"`
	Text     string  `json:"text,omitempty"`
}

func prepareMJPEGCapabilities(raw json.RawMessage) (json.RawMessage, int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	var envelope map[string]any
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, 0, errors.New("无效的 iOS Session 请求")
	}
	capabilities, ok := envelope["capabilities"].(map[string]any)
	if !ok {
		return nil, 0, errors.New("缺少 iOS Session capabilities")
	}
	candidates, ok := capabilities["firstMatch"].([]any)
	if !ok || len(candidates) != 1 {
		return nil, 0, errors.New("iOS Session firstMatch 无效")
	}
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		return nil, 0, errors.New("iOS Session firstMatch 无效")
	}
	candidate["appium:mjpegServerPort"] = port
	encoded, err := json.Marshal(envelope)
	return encoded, port, err
}

func (server *Server) remote(writer http.ResponseWriter, request *http.Request) {
	if !constantBearer(request.Header.Get("Authorization"), server.config.AgentToken) ||
		request.Header.Get("X-Device-Farm-Host-Id") != server.config.HostID {
		http.Error(writer, "iOS 远控请求未授权", http.StatusForbidden)
		return
	}
	remainder := strings.TrimPrefix(request.URL.Path, "/internal/v1/ios-remote/sessions/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || !validAppiumSessionID(parts[0]) {
		http.NotFound(writer, request)
		return
	}
	sessionID, operation := parts[0], parts[1]
	switch {
	case request.Method == http.MethodGet && operation == "stream":
		server.remoteStream(writer, request, sessionID)
	case request.Method == http.MethodGet && operation == "frame":
		server.remoteFrame(writer, request, sessionID)
	case request.Method == http.MethodGet && operation == "health":
		server.remoteHealth(writer, request, sessionID)
	case request.Method == http.MethodPost && operation == "actions":
		server.remoteAction(writer, request, sessionID)
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) remoteStream(writer http.ResponseWriter, request *http.Request, sessionID string) {
	port, err := server.mjpegPort(request.Context(), sessionID)
	if err != nil {
		http.Error(writer, "iOS 实时画面尚未就绪", http.StatusServiceUnavailable)
		return
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/"
	upstreamRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		http.Error(writer, "无法创建 iOS 画面请求", http.StatusBadGateway)
		return
	}
	response, err := server.streamClient.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "iOS 实时画面暂时不可用", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 ||
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "multipart/x-mixed-replace") {
		http.Error(writer, "iOS 实时画面响应无效", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(writer, response.Body)
}

// mjpegPort first uses the binding created in this Fence process. After a
// Fence restart that in-memory binding is gone while Appium/WDA may still be
// healthy, so recover the exact port from the bound Appium Session
// capabilities instead of forcing the user session to end.
func (server *Server) mjpegPort(ctx context.Context, sessionID string) (int, error) {
	if value, ok := server.remoteSessions.Load(sessionID); ok {
		if port, valid := value.(int); valid && port > 0 && port <= 65535 {
			return port, nil
		}
	}
	response, body, err := server.callUpstream(ctx, http.MethodGet,
		"/session/"+url.PathEscape(sessionID), nil, "", 256<<10)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, errors.New("Appium Session 不可用")
	}
	var envelope struct {
		Value map[string]any `json:"value"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return 0, errors.New("Appium Session capabilities 无效")
	}
	capabilities := envelope.Value
	if nested, ok := envelope.Value["capabilities"].(map[string]any); ok {
		capabilities = nested
	}
	port, ok := numericPort(capabilities["appium:mjpegServerPort"])
	if !ok {
		port, ok = numericPort(capabilities["mjpegServerPort"])
	}
	if !ok {
		return 0, errors.New("Appium Session 未返回 MJPEG 端口")
	}
	server.remoteSessions.Store(sessionID, port)
	return port, nil
}

func numericPort(value any) (int, bool) {
	var port int
	switch typed := value.(type) {
	case float64:
		if typed != math.Trunc(typed) {
			return 0, false
		}
		port = int(typed)
	case int:
		port = typed
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		port = parsed
	default:
		return 0, false
	}
	return port, port > 0 && port <= 65535
}

func (server *Server) remoteFrame(writer http.ResponseWriter, request *http.Request, sessionID string) {
	response, body, err := server.callUpstream(request.Context(), http.MethodGet,
		"/session/"+url.PathEscape(sessionID)+"/screenshot", nil, "", maxCreateReply)
	if err != nil {
		http.Error(writer, "iOS 截图暂时不可用", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(writer, "iOS 截图暂时不可用", http.StatusBadGateway)
		return
	}
	var envelope struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		http.Error(writer, "iOS 截图响应无效", http.StatusBadGateway)
		return
	}
	image, err := base64.StdEncoding.DecodeString(envelope.Value)
	if err != nil || len(image) == 0 || len(image) > maxCreateReply {
		http.Error(writer, "iOS 截图响应无效", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(image)
}

func (server *Server) remoteHealth(writer http.ResponseWriter, request *http.Request, sessionID string) {
	response, err := server.streamUpstream(request.Context(), http.MethodGet,
		"/session/"+url.PathEscape(sessionID)+"/window/rect", nil, "")
	if err != nil {
		http.Error(writer, "iOS 远控 Session 不可用", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(writer, "iOS 远控 Session 不可用", http.StatusBadGateway)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) remoteAction(writer http.ResponseWriter, request *http.Request, sessionID string) {
	raw, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRemoteActionBody))
	if err != nil {
		http.Error(writer, "iOS 远控操作过大", http.StatusRequestEntityTooLarge)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var action remoteAction
	if decoder.Decode(&action) != nil {
		http.Error(writer, "iOS 远控操作无效", http.StatusBadRequest)
		return
	}
	method, path, payload, err := server.translateRemoteAction(request.Context(), sessionID, action)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	response, body, err := server.callUpstream(request.Context(), method, path, payload, "application/json", 1<<20)
	if err != nil {
		http.Error(writer, "iOS 远控操作暂时不可用", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		server.config.Logger.Warn("iOS remote action rejected by Appium", "status", response.StatusCode, "action", action.Type)
		http.Error(writer, "iOS 远控操作执行失败", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	if len(body) == 0 {
		body = []byte(`{"value":null}`)
	}
	_, _ = writer.Write(body)
}

func (server *Server) translateRemoteAction(ctx context.Context, sessionID string, action remoteAction) (string, string, []byte, error) {
	path := "/session/" + url.PathEscape(sessionID)
	switch strings.ToLower(strings.TrimSpace(action.Type)) {
	case "tap", "swipe":
		width, height, err := server.remoteWindowSize(ctx, sessionID)
		if err != nil {
			return "", "", nil, errors.New("无法读取 iOS 画面尺寸")
		}
		if !normalized(action.X) || !normalized(action.Y) {
			return "", "", nil, errors.New("iOS 远控坐标无效")
		}
		actions := []map[string]any{{"type": "pointerMove", "duration": 0,
			"x": int(math.Round(action.X * float64(width))), "y": int(math.Round(action.Y * float64(height)))},
			{"type": "pointerDown", "button": 0}}
		if strings.EqualFold(action.Type, "swipe") {
			if !normalized(action.EndX) || !normalized(action.EndY) {
				return "", "", nil, errors.New("iOS 远控滑动坐标无效")
			}
			duration := action.Duration
			if duration == 0 {
				duration = 500
			}
			if duration < 100 || duration > 2000 {
				return "", "", nil, errors.New("iOS 远控滑动时长无效")
			}
			actions = append(actions, map[string]any{"type": "pause", "duration": 80},
				map[string]any{"type": "pointerMove", "duration": duration,
					"x": int(math.Round(action.EndX * float64(width))), "y": int(math.Round(action.EndY * float64(height)))})
		}
		actions = append(actions, map[string]any{"type": "pointerUp", "button": 0})
		payload, _ := json.Marshal(map[string]any{"actions": []any{map[string]any{
			"type": "pointer", "id": "finger", "parameters": map[string]string{"pointerType": "touch"}, "actions": actions,
		}}})
		return http.MethodPost, path + "/actions", payload, nil
	case "text":
		if len(action.Text) == 0 || len([]rune(action.Text)) > 200 || strings.ContainsRune(action.Text, '\x00') {
			return "", "", nil, errors.New("iOS 远控输入内容无效")
		}
		characters := make([]string, 0, len([]rune(action.Text)))
		for _, character := range action.Text {
			characters = append(characters, string(character))
		}
		payload, _ := json.Marshal(map[string]any{"text": action.Text, "value": characters})
		return http.MethodPost, path + "/keys", payload, nil
	case "home":
		payload := []byte(`{"script":"mobile: pressButton","args":[{"name":"home"}]}`)
		return http.MethodPost, path + "/execute/sync", payload, nil
	default:
		return "", "", nil, fmt.Errorf("不支持的 iOS 远控操作")
	}
}

func (server *Server) remoteWindowSize(ctx context.Context, sessionID string) (int, int, error) {
	response, body, err := server.callUpstream(ctx, http.MethodGet,
		"/session/"+url.PathEscape(sessionID)+"/window/rect", nil, "", 64<<10)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	var envelope struct {
		Value struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"value"`
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || json.Unmarshal(body, &envelope) != nil ||
		envelope.Value.Width < 1 || envelope.Value.Height < 1 {
		return 0, 0, errors.New("invalid Appium window size")
	}
	return envelope.Value.Width, envelope.Value.Height, nil
}

func normalized(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
