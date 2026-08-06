package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
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
	if !handler.enforceOwnerInput(writer, request, &input.OwnerType, &input.OwnerID) || !handler.authorizeReservation(writer, request, request.PathValue("id")) {
		return
	}
	value, err := handler.service.CreateRemoteSession(
		request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"), request.PathValue("id"), input,
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
	if !handler.authorizeReservation(writer, request, request.PathValue("id")) {
		return
	}
	value, err := handler.service.Extend(
		request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"),
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
	if !handler.authorizeReservation(writer, request, request.PathValue("id")) {
		return
	}
	if principal, ok := auth.FromContext(request.Context()); ok && principal.Role == auth.RoleConsole && input.Force && principal.ConsoleRole != auth.ConsoleAdmin {
		writeForbidden(writer, request, "only console admins can force release reservations")
		return
	}
	value, err := handler.service.Release(
		request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"),
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
	if !handler.enforceOwnerInput(writer, request, &input.OwnerType, &input.OwnerID) {
		return
	}
	value, err := handler.service.Create(request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusCreated, value, err)
}

func (handler *reservationHandler) list(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	filter := reservation.Filter{
		OwnerType: request.URL.Query().Get("owner_type"),
		OwnerID:   request.URL.Query().Get("owner_id"),
	}
	if principal, exists := auth.FromContext(request.Context()); exists && principal.Role == auth.RoleConsole && principal.ConsoleRole != auth.ConsoleAdmin {
		filter.OwnerType, filter.OwnerID = "manual", principal.SubjectID
	}
	value, err := handler.service.List(request.Context(), filter, page)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *reservationHandler) get(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.Get(request.Context(), request.PathValue("id"))
	if err == nil && !reservationVisible(request, value) {
		writeForbidden(writer, request, "reservation owner does not match")
		return
	}
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *reservationHandler) enforceOwnerInput(writer http.ResponseWriter, request *http.Request, ownerType, ownerID *string) bool {
	principal, ok := auth.FromContext(request.Context())
	if !ok || principal.Role != auth.RoleConsole {
		return true
	}
	if (*ownerType != "" && *ownerType != "manual") || (*ownerID != "" && *ownerID != principal.SubjectID) {
		writeForbidden(writer, request, "console reservation owner is determined by the authenticated session")
		return false
	}
	*ownerType, *ownerID = "manual", principal.SubjectID
	return true
}

func (handler *reservationHandler) authorizeReservation(writer http.ResponseWriter, request *http.Request, id string) bool {
	principal, ok := auth.FromContext(request.Context())
	if !ok || principal.Role != auth.RoleConsole || principal.ConsoleRole == auth.ConsoleAdmin {
		return true
	}
	value, err := handler.service.Get(request.Context(), id)
	if err != nil {
		handler.write(writer, request, http.StatusOK, nil, err)
		return false
	}
	if !reservationVisible(request, value) {
		writeForbidden(writer, request, "reservation owner does not match")
		return false
	}
	return true
}

func reservationVisible(request *http.Request, value reservation.View) bool {
	principal, ok := auth.FromContext(request.Context())
	if !ok || principal.Role != auth.RoleConsole || principal.ConsoleRole == auth.ConsoleAdmin {
		return true
	}
	return value.OwnerType == "manual" && value.OwnerID == principal.SubjectID
}

func writeForbidden(writer http.ResponseWriter, request *http.Request, message string) {
	httpx.WriteError(writer, request, http.StatusForbidden, httpx.APIError{Code: "FORBIDDEN", Message: message, Retryable: false})
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
