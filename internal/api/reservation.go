package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
)

type reservationHandler struct{ service *reservation.Service }

func RegisterReservations(mux *http.ServeMux, service *reservation.Service) {
	handler := &reservationHandler{service: service}
	mux.HandleFunc("GET /api/v1/device-reservations", handler.list)
	mux.HandleFunc("POST /api/v1/device-reservations", handler.create)
	mux.HandleFunc("GET /api/v1/device-reservations/{id}", handler.get)
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/extensions", handler.extend)
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/releases", handler.release)
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/remote-sessions", handler.createRemoteSession)
}

func (handler *reservationHandler) createRemoteSession(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input reservation.RemoteSessionInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.CreateRemoteSession(
		request.Context(), clientID(request), request.Header.Get("Idempotency-Key"), request.PathValue("id"), input,
	)
	handler.write(writer, request, http.StatusCreated, value, err)
}

func (handler *reservationHandler) extend(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input reservation.ExtensionInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Extend(
		request.Context(), clientID(request), request.Header.Get("Idempotency-Key"),
		request.PathValue("id"), input,
	)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *reservationHandler) release(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input reservation.ReleaseInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Release(
		request.Context(), clientID(request), request.Header.Get("Idempotency-Key"),
		request.PathValue("id"), correlation.FromContext(request.Context()).RequestID, input,
	)
	handler.write(writer, request, http.StatusOK, value, err)
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
		httpStatus, apiError = http.StatusConflict, httpx.APIError{Code: "DEVICE_POOL_UNAVAILABLE", Message: err.Error(), Retryable: false}
	case errors.Is(err, reservation.ErrCapacityUnavailable):
		httpStatus, apiError = http.StatusServiceUnavailable, httpx.APIError{Code: "DEVICE_CAPACITY_UNAVAILABLE", Message: err.Error(), Retryable: true}
	case errors.Is(err, reservation.ErrForbidden):
		httpStatus, apiError = http.StatusForbidden, httpx.APIError{Code: "FORBIDDEN", Message: "reservation owner does not match"}
	case errors.Is(err, reservation.ErrSTFReleaseFailed):
		httpStatus, apiError = http.StatusBadGateway, httpx.APIError{Code: "STF_RELEASE_FAILED", Message: "STF release failed; reservation remains active", Retryable: isRetryable(err)}
	case errors.Is(err, reservation.ErrSTFRemoteFailed):
		httpStatus, apiError = http.StatusBadGateway, httpx.APIError{Code: "STF_REMOTE_CONNECT_FAILED", Message: "STF remote connection is unavailable", Retryable: isRetryable(err)}
	}
	httpx.WriteError(writer, request, httpStatus, apiError)
}

func isRetryable(err error) bool {
	type retryable interface{ IsRetryable() bool }
	var value retryable
	return errors.As(err, &value) && value.IsRetryable()
}
