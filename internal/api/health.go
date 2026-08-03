package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
)

type healthHandler struct{ service *reconcile.Service }

func RegisterHealth(mux *http.ServeMux, service *reconcile.Service) {
	handler := &healthHandler{service: service}
	mux.HandleFunc("POST /internal/v1/devices/{id}/health-events", handler.report)
}

func (handler *healthHandler) report(writer http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{
			Code: "SERVICE_UNAVAILABLE", Message: "health service is not configured", Retryable: true,
		})
		return
	}
	var input reconcile.EventInput
	if !decode(writer, request, &input) {
		return
	}
	if input.Source == "reconciler" {
		writeInvalid(writer, request, "source reconciler is reserved for the server")
		return
	}
	value, err := handler.service.Report(request.Context(), request.PathValue("id"), input)
	if err == nil {
		httpx.WriteData(writer, request, http.StatusCreated, value)
		return
	}
	switch {
	case errors.Is(err, reconcile.ErrInvalidArgument):
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()})
	case errors.Is(err, reconcile.ErrNotFound):
		httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: "device not found"})
	default:
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL_ERROR", Message: "internal server error"})
	}
}
