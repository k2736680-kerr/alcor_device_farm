package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossimulator"
)

type iosSimulatorHandler struct{ service *iossimulator.Service }

func RegisterIOSSimulators(mux *http.ServeMux, service *iossimulator.Service) {
	handler := &iosSimulatorHandler{service: service}
	mux.HandleFunc("GET /api/v1/ios-simulator-catalog", handler.catalog)
	mux.HandleFunc("POST /api/v1/ios-simulators", handler.create)
}

func (handler *iosSimulatorHandler) catalog(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.Catalog(request.Context(), strings.TrimSpace(request.URL.Query().Get("host_id")))
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *iosSimulatorHandler) create(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input iossimulator.CreateInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Create(request.Context(), requestActor(request),
		correlation.FromContext(request.Context()).RequestID, request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusAccepted, value, err)
}

func (handler *iosSimulatorHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{
		Code: "SERVICE_UNAVAILABLE", Message: "iOS 模拟器管理服务尚未配置", Retryable: true,
	})
	return false
}

func (handler *iosSimulatorHandler) write(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	httpStatus := http.StatusInternalServerError
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "服务器内部错误", Retryable: false}
	switch {
	case errors.Is(err, iossimulator.ErrInvalidArgument):
		httpStatus, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	case errors.Is(err, iossimulator.ErrNotFound):
		httpStatus, apiError = http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: err.Error()}
	case errors.Is(err, iossimulator.ErrCapacity):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "INSUFFICIENT_HOST_RESOURCES", Message: err.Error(), Retryable: true}
		var capacityError *iossimulator.CapacityError
		if errors.As(err, &capacityError) {
			apiError.Details = capacityError.Result
		}
	case errors.Is(err, iossimulator.ErrConflict):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "CONFLICT", Message: err.Error()}
	}
	httpx.WriteError(writer, request, httpStatus, apiError)
}
