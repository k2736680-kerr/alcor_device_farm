package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

type reservationHandler struct{ service *reservation.Service }

func RegisterReservations(mux *http.ServeMux, service *reservation.Service) {
	handler := &reservationHandler{service: service}
	mux.HandleFunc("GET /api/v1/device-reservations", handler.list)
	mux.HandleFunc("POST /api/v1/device-reservations", handler.create)
	mux.HandleFunc("GET /api/v1/device-reservations/{id}", handler.get)
}

func (handler *reservationHandler) create(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input reservation.CreateInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Create(request.Context(), clientID(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusCreated, value, err)
}

func (handler *reservationHandler) list(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	values, err := handler.service.List(request.Context(), reservation.Filter{
		OwnerType: request.URL.Query().Get("owner_type"),
		OwnerID:   request.URL.Query().Get("owner_id"),
	})
	handler.write(writer, request, http.StatusOK, map[string]any{"items": values}, err)
}

func (handler *reservationHandler) get(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.Get(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *reservationHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{
		Code: "SERVICE_UNAVAILABLE", Message: "reservation service is not configured", Retryable: true,
	})
	return false
}

func (handler *reservationHandler) write(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "internal server error"}
	httpStatus := http.StatusInternalServerError
	switch {
	case errors.Is(err, reservation.ErrInvalidArgument):
		httpStatus, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	case errors.Is(err, reservation.ErrNotFound):
		httpStatus, apiError = http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: "reservation not found"}
	case errors.Is(err, reservation.ErrConflict):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "CONFLICT", Message: err.Error()}
	case errors.Is(err, reservation.ErrPoolUnavailable):
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "POOL_UNAVAILABLE", Message: err.Error(), Retryable: false}
	case errors.Is(err, reservation.ErrCapacityUnavailable):
		httpStatus, apiError = http.StatusServiceUnavailable, httpx.APIError{Code: "CAPACITY_UNAVAILABLE", Message: err.Error(), Retryable: true}
	}
	httpx.WriteError(writer, request, httpStatus, apiError)
}
