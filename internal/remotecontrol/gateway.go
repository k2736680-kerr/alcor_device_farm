package remotecontrol

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

const iosGatewayPrefix = "/console/remote/ios/"

func RegisterGateway(mux *http.ServeMux, service *Service, loggers ...*slog.Logger) {
	if mux == nil || service == nil || !service.config.IOSEnabled || service.iosSessions == nil {
		return
	}
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	handler := &gatewayHandler{service: service, logger: logger}
	mux.Handle("GET "+iosGatewayPrefix+"{token}/{asset}", handler)
	mux.Handle("HEAD "+iosGatewayPrefix+"{token}/{asset}", handler)
	mux.Handle("POST "+iosGatewayPrefix+"{token}/{asset}", handler)
}

type gatewayHandler struct {
	service *Service
	logger  *slog.Logger
}

func (handler *gatewayHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	principal, ok := auth.FromContext(request.Context())
	ownerID := ""
	if ok && principal.Role == auth.RoleConsole && principal.ConsoleRole == auth.ConsoleAdmin {
		ownerID = principal.SubjectID
	} else if ok && principal.Role == auth.RoleService {
		// 仅可信平台服务可以代理嵌入式控制页；操作者仍由已认证的
		// Alcor 服务端写入审计头，浏览器无法自行选择预约所有者。
		ownerID = strings.TrimSpace(request.Header.Get("X-Device-Farm-Actor-Id"))
	}
	if ownerID == "" {
		writeGatewayError(writer, request, http.StatusForbidden, "当前控制台角色无权使用 iOS 远程控制")
		return
	}
	asset := request.PathValue("asset")
	// The signed URL is a short-lived entry ticket. Once the owning Console
	// session has loaded the page, every asset, stream and action is protected
	// by that Console session plus the still-active Reservation. Expiring the
	// ticket on subresources would break a healthy long-running remote session.
	checkExpiry := asset == "control"
	claims, err := handler.service.verifyGatewayToken(request.PathValue("token"), ownerID, checkExpiry)
	if err != nil {
		writeGatewayError(writer, request, http.StatusUnauthorized, "iOS 远控入口已失效，请返回设备页面重新打开")
		return
	}
	if err := handler.service.gatewayReservationActive(request.Context(), claims); err != nil {
		writeGatewayError(writer, request, http.StatusConflict, "iOS 远控预约已结束，请返回设备页面刷新")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	switch {
	case request.Method == http.MethodGet && asset == "control":
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(writer, iosRemoteHTML)
	case request.Method == http.MethodGet && asset == "client.js":
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = io.WriteString(writer, iosRemoteJavaScript)
	case request.Method == http.MethodGet && asset == "style.css":
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = io.WriteString(writer, iosRemoteCSS)
	case request.Method == http.MethodGet && (asset == "stream" || asset == "frame"):
		handler.proxyIOS(writer, request, claims, http.MethodGet, asset, nil)
	case request.Method == http.MethodPost && asset == "actions":
		raw, readErr := io.ReadAll(http.MaxBytesReader(writer, request.Body, 16<<10))
		if readErr != nil {
			writeGatewayError(writer, request, http.StatusBadRequest, "iOS 远控操作无效")
			return
		}
		handler.proxyIOS(writer, request, claims, http.MethodPost, asset, raw)
	default:
		writeGatewayError(writer, request, http.StatusNotFound, "未找到 iOS 远控资源")
	}
}

func (handler *gatewayHandler) proxyIOS(writer http.ResponseWriter, request *http.Request, claims gatewayClaims, method, operation string, body []byte) {
	binding, err := handler.service.iosBinding(request.Context(), claims)
	if err != nil {
		writeGatewayError(writer, request, http.StatusConflict, "iOS 远控 Session 尚未就绪")
		return
	}
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	if operation == "stream" {
		go handler.monitorReservation(ctx, cancel, claims)
	}
	response, err := handler.service.iosFenceRequest(ctx, binding, method, operation, body)
	if err != nil {
		handler.logger.ErrorContext(request.Context(), "iOS 目标 Simulator 远控连接失败", "operation", operation, "error", err)
		writeGatewayError(writer, request, http.StatusBadGateway, "iOS 远程画面或操作暂时不可用，请稍后重试")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		status := http.StatusBadGateway
		if response.StatusCode == http.StatusBadRequest {
			status = http.StatusBadRequest
		}
		writeGatewayError(writer, request, status, "iOS 远程画面或操作执行失败")
		return
	}
	for _, name := range []string{"Content-Type", "X-Content-Type-Options"} {
		if value := response.Header.Get(name); value != "" {
			writer.Header().Set(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (handler *gatewayHandler) monitorReservation(ctx context.Context, cancel context.CancelFunc, claims gatewayClaims) {
	interval := handler.service.config.Heartbeat
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
			err := handler.service.gatewayReservationActive(checkCtx, claims)
			checkCancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (service *Service) verifyGatewayToken(token, ownerID string, checkExpiry bool) (gatewayClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return gatewayClaims{}, ErrUnavailable
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return gatewayClaims{}, ErrUnavailable
	}
	mac := hmac.New(sha256.New, []byte(service.config.IOSGatewaySecret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return gatewayClaims{}, ErrUnavailable
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return gatewayClaims{}, ErrUnavailable
	}
	var claims gatewayClaims
	if json.Unmarshal(payload, &claims) != nil || claims.DeviceID == "" || claims.ReservationID == "" ||
		claims.OwnerID == "" || claims.OwnerID != ownerID || claims.ExpiresAt <= 0 {
		return gatewayClaims{}, ErrUnavailable
	}
	if checkExpiry && !service.config.Now().Before(time.Unix(claims.ExpiresAt, 0)) {
		return gatewayClaims{}, ErrUnavailable
	}
	return claims, nil
}

func (service *Service) gatewayReservationActive(ctx context.Context, claims gatewayClaims) error {
	current, err := service.reservations.FindOpenForDevice(ctx, claims.OwnerID, claims.DeviceID)
	if err != nil || current.ID != claims.ReservationID || current.Status != domain.ReservationActive ||
		current.DeviceID == nil || *current.DeviceID != claims.DeviceID ||
		(current.ExpiresAt != nil && !current.ExpiresAt.After(service.config.Now())) {
		return ErrConflict
	}
	return nil
}

func writeGatewayError(writer http.ResponseWriter, request *http.Request, status int, message string) {
	httpx.WriteError(writer, request, status, httpx.APIError{
		Code: "IOS_REMOTE_CONTROL_UNAVAILABLE", Message: message, Retryable: status >= 500,
	})
}

const iosRemoteHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>iOS 模拟器远程控制</title><link rel="stylesheet" href="./style.css"></head>
<body><main><header><div><h1>iOS 模拟器远程控制</h1><p id="status">正在连接目标模拟器…</p></div><button id="home" type="button">主屏幕</button></header>
<section class="device"><img id="screen" src="./stream" alt="目标 iOS 模拟器实时画面" draggable="false"></section>
<form id="text-form"><label for="text">输入文字</label><input id="text" maxlength="200" autocomplete="off" placeholder="先在模拟器中点选输入框"><button type="submit">发送</button></form>
<p class="hint">在画面上点击或拖动即可操作。这里仅显示当前预约的模拟器，不会显示 Mac 桌面。</p></main><script src="./client.js"></script></body></html>`

const iosRemoteCSS = `:root{color-scheme:dark;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b0f17;color:#f5f7fb}*{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at top,#17233a,#090c12 58%)}main{width:min(100%,720px);margin:0 auto;padding:20px}header{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:16px}h1{font-size:20px;margin:0 0 5px}p{margin:0;color:#aab4c7;font-size:13px}button,input{font:inherit}button{border:0;border-radius:10px;padding:10px 16px;background:#3178ff;color:white;cursor:pointer}.device{display:flex;justify-content:center;min-height:420px;padding:12px;border:1px solid #273247;border-radius:18px;background:#030508;box-shadow:0 20px 70px #0008}#screen{display:block;max-width:100%;max-height:calc(100vh - 210px);border-radius:10px;touch-action:none;user-select:none;background:#000;cursor:crosshair}form{display:grid;grid-template-columns:auto 1fr auto;align-items:center;gap:10px;margin-top:14px}input{min-width:0;border:1px solid #34415a;border-radius:10px;padding:10px 12px;background:#111827;color:white}.hint{margin-top:12px;text-align:center}#status.error{color:#ff8f8f}@media(max-width:520px){main{padding:12px}header{align-items:flex-start}form{grid-template-columns:1fr auto}form label{grid-column:1/-1}}`

const iosRemoteJavaScript = `(()=>{const screen=document.getElementById('screen'),status=document.getElementById('status'),home=document.getElementById('home'),form=document.getElementById('text-form'),text=document.getElementById('text');let start=null,retries=0;const cookie=name=>document.cookie.split(';').map(v=>v.trim()).find(v=>v.startsWith(name+'='))?.slice(name.length+1)||'';const send=async body=>{const headers={'Content-Type':'application/json'},csrf=cookie('device_farm_csrf');if(csrf)headers['X-CSRF-Token']=decodeURIComponent(csrf);const response=await fetch('./actions',{method:'POST',headers,body:JSON.stringify(body)});if(!response.ok)throw new Error('操作失败');status.textContent='已连接目标模拟器';status.className=''};const point=e=>{const r=screen.getBoundingClientRect();return{x:Math.max(0,Math.min(1,(e.clientX-r.left)/r.width)),y:Math.max(0,Math.min(1,(e.clientY-r.top)/r.height))}};screen.addEventListener('load',()=>{retries=0;status.textContent='已连接目标模拟器';status.className=''});screen.addEventListener('error',()=>{status.textContent=retries<10?'实时画面正在重连…':'实时画面暂时中断，请返回设备页面重试';status.className='error';if(retries++<10)setTimeout(()=>{screen.src='./stream?retry='+Date.now()},1500)});screen.addEventListener('pointerdown',e=>{e.preventDefault();screen.setPointerCapture(e.pointerId);start={...point(e),at:Date.now()}});screen.addEventListener('pointerup',e=>{if(!start)return;const end=point(e),distance=Math.hypot(end.x-start.x,end.y-start.y),duration=Math.max(100,Math.min(2000,Date.now()-start.at));const body=distance<.015?{type:'tap',x:end.x,y:end.y}:{type:'swipe',x:start.x,y:start.y,end_x:end.x,end_y:end.y,duration_ms:duration};start=null;send(body).catch(()=>{status.textContent='操作失败，请稍后重试';status.className='error'})});screen.addEventListener('pointercancel',()=>{start=null});home.addEventListener('click',()=>send({type:'home'}).catch(()=>{status.textContent='主屏幕操作失败';status.className='error'}));form.addEventListener('submit',e=>{e.preventDefault();if(!text.value)return;send({type:'text',text:text.value}).then(()=>{text.value=''}).catch(()=>{status.textContent='文字发送失败，请先点选输入框';status.className='error'})})})();`
