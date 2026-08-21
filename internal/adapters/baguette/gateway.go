package baguette

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const sessionCookiePrefix = "device_farm_baguette_session_"

type Authorizer interface {
	AuthorizeBaguette(context.Context, Ticket) error
}

type Gateway struct {
	client     *Client
	authorizer Authorizer
	proxy      *httputil.ReverseProxy
	checkEvery time.Duration
}

func NewGateway(client *Client, authorizer Authorizer) (*Gateway, error) {
	if client == nil || authorizer == nil {
		return nil, ErrUnavailable
	}
	proxy := httputil.NewSingleHostReverseProxy(client.upstream)
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "iOS 远程控制连接暂时不可用", http.StatusBadGateway)
	}
	return &Gateway{client: client, authorizer: authorizer, proxy: proxy, checkEvery: 5 * time.Second}, nil
}

func (gateway *Gateway) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(request.URL.Path, "/entry/") {
		gateway.entry(writer, request)
		return
	}
	ticket, err := gateway.session(request)
	if err != nil || gateway.authorizer.AuthorizeBaguette(request.Context(), ticket) != nil {
		http.Error(writer, "iOS 远程控制预约已失效，请返回设备页面重新打开", http.StatusUnauthorized)
		return
	}
	if request.URL.Path == "/" || request.URL.Path == "/simulators" {
		http.Redirect(writer, request, "/simulators/"+url.PathEscape(ticket.UDID), http.StatusFound)
		return
	}
	if request.URL.Path == "/simulators.json" {
		gateway.filteredInventory(writer, request, ticket)
		return
	}
	if request.URL.Path == "/plugins.json" && request.Method == http.MethodGet {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"plugins":[]}`))
		return
	}
	if !allowedPath(request, ticket.UDID) {
		http.Error(writer, "当前预约无权访问该 iOS 设备功能", http.StatusForbidden)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead || isUpgrade(request) {
		expectedOrigin := gateway.client.public.Scheme + "://" + gateway.client.public.Host
		if request.Header.Get("Origin") != expectedOrigin {
			http.Error(writer, "iOS 远程控制请求来源校验失败", http.StatusForbidden)
			return
		}
	}
	request.Header.Set("Origin", gateway.client.upstream.Scheme+"://"+gateway.client.upstream.Host)
	request.Host = gateway.client.upstream.Host
	if isUpgrade(request) {
		ctx, cancel := context.WithCancel(request.Context())
		defer cancel()
		request = request.WithContext(ctx)
		go gateway.monitor(ctx, cancel, ticket)
	}
	gateway.proxy.ServeHTTP(writer, request)
}

func (gateway *Gateway) entry(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "不支持当前请求方法", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(request.URL.Path, "/entry/")
	ticket, err := gateway.client.verify(token, true)
	if err != nil || gateway.authorizer.AuthorizeBaguette(request.Context(), ticket) != nil {
		http.Error(writer, "iOS 远程控制入口已失效，请返回设备页面重新打开", http.StatusUnauthorized)
		return
	}
	// 会话 Cookie 不设固定到期时间。每个请求和长连接都以数据库中的
	// active Reservation 为准，前端心跳持续续约，结束后立即失效。
	session, err := gateway.client.sign(Ticket{DeviceID: ticket.DeviceID, UDID: ticket.UDID,
		ReservationID: ticket.ReservationID, OwnerID: ticket.OwnerID})
	if err != nil {
		http.Error(writer, "无法建立 iOS 远程控制连接", http.StatusInternalServerError)
		return
	}
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName(ticket.UDID), Value: session, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: gateway.client.public.Scheme == "https"})
	http.Redirect(writer, request, "/simulators/"+url.PathEscape(ticket.UDID), http.StatusFound)
}

func (gateway *Gateway) session(request *http.Request) (Ticket, error) {
	if udid := requestTargetUDID(request, gateway.client.public); udid != "" {
		cookie, err := request.Cookie(sessionCookieName(udid))
		if err != nil {
			return Ticket{}, ErrUnauthorized
		}
		ticket, err := gateway.client.verify(cookie.Value, false)
		if err != nil || ticket.UDID != udid {
			return Ticket{}, ErrUnauthorized
		}
		return ticket, nil
	}
	var selected *Ticket
	for _, cookie := range request.Cookies() {
		if !strings.HasPrefix(cookie.Name, sessionCookiePrefix) {
			continue
		}
		ticket, err := gateway.client.verify(cookie.Value, false)
		if err != nil || cookie.Name != sessionCookieName(ticket.UDID) {
			continue
		}
		if selected != nil {
			return Ticket{}, ErrUnauthorized
		}
		copy := ticket
		selected = &copy
	}
	if selected == nil {
		return Ticket{}, ErrUnauthorized
	}
	return *selected, nil
}

func sessionCookieName(udid string) string {
	sum := sha256.Sum256([]byte(udid))
	return sessionCookiePrefix + base64.RawURLEncoding.EncodeToString(sum[:12])
}

func requestTargetUDID(request *http.Request, public *url.URL) string {
	if udid := targetUDIDFromPath(request.URL.Path); udid != "" {
		return udid
	}
	referer, err := url.Parse(request.Referer())
	if err != nil || referer.Scheme != public.Scheme || referer.Host != public.Host {
		return ""
	}
	return targetUDIDFromPath(referer.Path)
}

func targetUDIDFromPath(path string) string {
	const prefix = "/simulators/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	segment := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 2)[0]
	udid, err := url.PathUnescape(segment)
	if err != nil {
		return ""
	}
	return udid
}

func (gateway *Gateway) filteredInventory(writer http.ResponseWriter, request *http.Request, ticket Ticket) {
	endpoint := *gateway.client.upstream
	endpoint.Path += "/simulators.json"
	upstreamRequest, _ := http.NewRequestWithContext(request.Context(), http.MethodGet, endpoint.String(), nil)
	response, err := gateway.client.httpClient.Do(upstreamRequest)
	if err != nil {
		http.Error(writer, "无法读取 iOS 模拟器状态", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	var source inventory
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&source) != nil {
		http.Error(writer, "iOS 模拟器状态响应无效", http.StatusBadGateway)
		return
	}
	filtered := inventory{Running: make([]simulator, 0), Available: make([]simulator, 0)}
	for _, device := range source.Running {
		if device.UDID == ticket.UDID {
			filtered.Running = append(filtered.Running, device)
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(filtered)
}

func allowedPath(request *http.Request, udid string) bool {
	path := request.URL.Path
	target := "/simulators/" + url.PathEscape(udid)
	if path == target {
		return request.Method == http.MethodGet
	}
	if strings.HasPrefix(path, target+"/") {
		operation := strings.TrimPrefix(path, target+"/")
		if operation == "boot" || operation == "shutdown" {
			return false
		}
		return true
	}
	if request.Method != http.MethodGet {
		return false
	}
	if strings.HasPrefix(path, "/farm") || strings.HasPrefix(path, "/plugins") ||
		strings.HasPrefix(path, "/bakeries") || strings.HasPrefix(path, "/grants") {
		return false
	}
	// Baguette 的原生页面由根路径静态资源组成；只允许无参数的只读资源。
	return !strings.HasPrefix(path, "/simulators/") && request.URL.RawQuery == "" &&
		(strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".svg") ||
			strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2"))
}

func (gateway *Gateway) monitor(ctx context.Context, cancel context.CancelFunc, ticket Ticket) {
	ticker := time.NewTicker(gateway.checkEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, stop := context.WithTimeout(context.WithoutCancel(ctx), gateway.checkEvery)
			err := gateway.authorizer.AuthorizeBaguette(check, ticket)
			stop()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func isUpgrade(request *http.Request) bool {
	return strings.EqualFold(request.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(request.Header.Get("Connection")), "upgrade")
}
