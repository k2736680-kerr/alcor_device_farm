package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

type hostCommandHandler struct{ service *hostcommand.Service }

func RegisterHostCommands(mux *http.ServeMux, service *hostcommand.Service) {
	handler := &hostCommandHandler{service: service}
	mux.HandleFunc("POST /internal/v1/device-hosts/{id}/heartbeats", handler.heartbeat)
	mux.HandleFunc("POST /internal/v1/device-hosts/{id}/commands/claims", handler.claim)
	mux.HandleFunc("POST /internal/v1/device-host-commands/{id}/completions", handler.complete)
}

func (handler *hostCommandHandler) heartbeat(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input hostcommand.HeartbeatInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Heartbeat(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *hostCommandHandler) claim(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input hostcommand.ClaimInput
	if !decode(writer, request, &input) {
		return
	}
	values, err := handler.service.Claim(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, map[string]any{"items": values}, err)
}

func (handler *hostCommandHandler) complete(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input hostcommand.CompletionInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Complete(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *hostCommandHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "host command service is not configured", Retryable: true})
	return false
}

func (handler *hostCommandHandler) write(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	switch {
	case errors.Is(err, hostcommand.ErrInvalidArgument):
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()})
	case errors.Is(err, hostcommand.ErrNotFound):
		httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: err.Error()})
	case errors.Is(err, hostcommand.ErrConflict):
		httpx.WriteError(writer, request, http.StatusConflict, httpx.APIError{Code: "STALE_COMMAND_LEASE", Message: err.Error()})
	default:
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL_ERROR", Message: "internal server error"})
	}
}
