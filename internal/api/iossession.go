package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossession"
)

type iosSessionHandler struct{ service *iossession.Service }

func RegisterIOSSessions(mux *http.ServeMux, service *iossession.Service) {
	handler := &iosSessionHandler{service: service}
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/session-grants", handler.issue)
	mux.HandleFunc("POST /internal/v1/ios-session-fence/grants/consumptions", handler.consume)
	mux.HandleFunc("POST /internal/v1/ios-session-fence/sessions/bindings", handler.bind)
	mux.HandleFunc("POST /internal/v1/ios-session-fence/sessions/authorizations", handler.authorize)
	mux.HandleFunc("POST /internal/v1/ios-session-fence/sessions/closures", handler.close)
	mux.HandleFunc("POST /internal/v1/ios-session-fence/sessions/failures", handler.fail)
}

func (handler *iosSessionHandler) issue(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	principal, ok := auth.FromContext(request.Context())
	if !ok || principal.Role != auth.RoleService {
		writeForbidden(writer, request, "only a trusted service can issue an iOS Session Grant")
		return
	}
	var input iossession.GrantInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Issue(request.Context(), requestActor(request), request.PathValue("id"),
		correlation.FromContext(request.Context()).RequestID, input)
	handler.write(writer, request, http.StatusCreated, value, err)
}

func (handler *iosSessionHandler) consume(writer http.ResponseWriter, request *http.Request) {
	var input iossession.ConsumeInput
	if !handler.decodeAgent(writer, request, &input) {
		return
	}
	value, err := handler.service.Consume(request.Context(), input, correlation.FromContext(request.Context()).RequestID)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *iosSessionHandler) bind(writer http.ResponseWriter, request *http.Request) {
	var input iossession.BindingInput
	if !handler.decodeAgent(writer, request, &input) {
		return
	}
	err := handler.service.Bind(request.Context(), input, correlation.FromContext(request.Context()).RequestID)
	handler.write(writer, request, http.StatusOK, map[string]bool{"bound": err == nil}, err)
}

func (handler *iosSessionHandler) authorize(writer http.ResponseWriter, request *http.Request) {
	var input iossession.AuthorizationInput
	if !handler.decodeAgent(writer, request, &input) {
		return
	}
	value, err := handler.service.Authorize(request.Context(), input)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *iosSessionHandler) close(writer http.ResponseWriter, request *http.Request) {
	var input iossession.BindingInput
	if !handler.decodeAgent(writer, request, &input) {
		return
	}
	err := handler.service.Close(request.Context(), input, correlation.FromContext(request.Context()).RequestID)
	handler.write(writer, request, http.StatusOK, map[string]bool{"closed": err == nil}, err)
}

func (handler *iosSessionHandler) fail(writer http.ResponseWriter, request *http.Request) {
	var input iossession.FailureInput
	if !handler.decodeAgent(writer, request, &input) {
		return
	}
	err := handler.service.RecordFailure(request.Context(), input, correlation.FromContext(request.Context()).RequestID)
	handler.write(writer, request, http.StatusOK, map[string]bool{"recorded": err == nil}, err)
}

func (handler *iosSessionHandler) decodeAgent(writer http.ResponseWriter, request *http.Request, input any) bool {
	if !handler.available(writer, request) {
		return false
	}
	principal, ok := auth.FromContext(request.Context())
	if !ok || principal.Role != auth.RoleAgent {
		writeForbidden(writer, request, "only a Host Agent can call the iOS Session Fence control API")
		return false
	}
	return decode(writer, request, input)
}

func (handler *iosSessionHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{
		Code: "SERVICE_UNAVAILABLE", Message: "iOS 会话服务尚未配置", Retryable: true,
	})
	return false
}

func (handler *iosSessionHandler) write(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	httpStatus := http.StatusInternalServerError
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "服务器内部错误"}
	switch {
	case errors.Is(err, iossession.ErrInvalidArgument):
		httpStatus, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	case errors.Is(err, iossession.ErrNotFound):
		httpStatus, apiError = http.StatusNotFound, httpx.APIError{Code: "IOS_SESSION_NOT_FOUND", Message: "未找到 iOS 会话绑定"}
	case errors.Is(err, iossession.ErrForbidden):
		httpStatus, apiError = http.StatusForbidden, httpx.APIError{Code: "FORBIDDEN", Message: "预约所有者不匹配"}
	case errors.Is(err, iossession.ErrGrantExpired):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "IOS_SESSION_GRANT_EXPIRED", Message: err.Error()}
	case errors.Is(err, iossession.ErrGrantConsumed):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "IOS_SESSION_GRANT_CONSUMED", Message: err.Error()}
	case errors.Is(err, iossession.ErrRoutingMismatch):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "IOS_SESSION_ROUTING_MISMATCH", Message: err.Error()}
	case errors.Is(err, iossession.ErrProviderBusy):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "IOS_PROVIDER_BUSY_DRIFT", Message: err.Error(), Retryable: false}
	case errors.Is(err, iossession.ErrProviderBusyConverging):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "IOS_PROVIDER_BUSY_CONVERGING", Message: err.Error(), Retryable: true}
	case errors.Is(err, iossession.ErrHostUnavailable):
		httpStatus, apiError = http.StatusServiceUnavailable, httpx.APIError{Code: "IOS_SESSION_FENCE_UNAVAILABLE", Message: err.Error(), Retryable: true}
	case errors.Is(err, iossession.ErrCleanupFailed):
		httpStatus, apiError = http.StatusBadGateway, httpx.APIError{Code: "IOS_SESSION_CLEANUP_FAILED", Message: err.Error(), Retryable: true}
	case errors.Is(err, iossession.ErrConflict):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "CONFLICT", Message: err.Error()}
	}
	httpx.WriteError(writer, request, httpStatus, apiError)
}
