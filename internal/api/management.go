package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
)

type managementHandler struct{ service *management.Service }

func RegisterManagement(mux *http.ServeMux, service *management.Service) {
	handler := &managementHandler{service: service}
	mux.HandleFunc("GET /api/v1/device-images", handler.listImages)
	mux.HandleFunc("POST /api/v1/device-images", handler.createImage)
	mux.HandleFunc("GET /api/v1/device-images/{id}", handler.getImage)
	mux.HandleFunc("PUT /api/v1/device-images/{id}", handler.updateImage)
	mux.HandleFunc("POST /api/v1/device-images/{id}/validations", handler.validateImage)

	mux.HandleFunc("GET /api/v1/device-hosts", handler.listHosts)
	mux.HandleFunc("POST /api/v1/device-hosts", handler.createHost)
	mux.HandleFunc("GET /api/v1/device-hosts/{id}", handler.getHost)
	mux.HandleFunc("PUT /api/v1/device-hosts/{id}", handler.updateHost)
	mux.HandleFunc("POST /api/v1/device-hosts/{id}/drains", handler.drainHost)
	mux.HandleFunc("DELETE /api/v1/device-hosts/{id}/drains", handler.undrainHost)

	mux.HandleFunc("GET /api/v1/device-pools", handler.listPools)
	mux.HandleFunc("POST /api/v1/device-pools", handler.createPool)
	mux.HandleFunc("GET /api/v1/device-pools/{id}", handler.getPool)
	mux.HandleFunc("PUT /api/v1/device-pools/{id}", handler.updatePool)
	mux.HandleFunc("GET /api/v1/device-pools/{id}/images", handler.listPoolImages)
	mux.HandleFunc("PUT /api/v1/device-pools/{id}/images/{image_id}", handler.setPoolImage)
	mux.HandleFunc("DELETE /api/v1/device-pools/{id}/images/{image_id}", handler.disablePoolImage)
	mux.HandleFunc("POST /api/v1/device-pools/{id}/devices", handler.addPoolDevice)
	mux.HandleFunc("DELETE /api/v1/device-pools/{id}/devices", handler.removePoolDevice)

	mux.HandleFunc("GET /api/v1/devices", handler.listDevices)
	mux.HandleFunc("GET /api/v1/devices/{id}", handler.getDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/restarts", handler.restartDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/rebuilds", handler.rebuildDevice)
	mux.HandleFunc("POST /api/v1/devices/{id}/quarantines", handler.quarantineDevice)
	mux.HandleFunc("DELETE /api/v1/devices/{id}/quarantines", handler.unquarantineDevice)
}

func (handler *managementHandler) listImages(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	value, err := handler.service.ListImages(request.Context(), page)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) createImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.ImageInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.CreateImage(request.Context(), clientID(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusCreated, value, err)
}
func (handler *managementHandler) getImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.GetImage(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) updateImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.ImageInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.UpdateImage(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) validateImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	if !requireIdempotencyKey(writer, request) {
		return
	}
	value, err := handler.service.StartImageValidation(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusAccepted, value, err)
}

func (handler *managementHandler) listHosts(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	value, err := handler.service.ListHosts(request.Context(), page)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) createHost(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.HostInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.CreateHost(request.Context(), clientID(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusCreated, value, err)
}
func (handler *managementHandler) getHost(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.GetHost(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) updateHost(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.HostInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.UpdateHost(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) drainHost(writer http.ResponseWriter, request *http.Request) {
	handler.setHostDrain(writer, request, true)
}
func (handler *managementHandler) undrainHost(writer http.ResponseWriter, request *http.Request) {
	handler.setHostDrain(writer, request, false)
}
func (handler *managementHandler) setHostDrain(writer http.ResponseWriter, request *http.Request, draining bool) {
	if !handler.available(writer, request) {
		return
	}
	var input reasonInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.SetHostDraining(request.Context(), request.PathValue("id"), draining, input.Reason)
	handler.write(writer, request, http.StatusOK, value, err)
}

func (handler *managementHandler) listPools(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	value, err := handler.service.ListPools(request.Context(), page)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) createPool(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.PoolInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.CreatePool(request.Context(), clientID(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusCreated, value, err)
}
func (handler *managementHandler) getPool(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.GetPool(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) updatePool(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.PoolInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.UpdatePool(request.Context(), request.PathValue("id"), input)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) listPoolImages(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	value, err := handler.service.ListPoolImages(request.Context(), request.PathValue("id"), page)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) setPoolImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	var input management.PoolImageInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.SetPoolImage(
		request.Context(), request.PathValue("id"), request.PathValue("image_id"), input,
		requestActor(request), correlation.FromContext(request.Context()).RequestID,
	)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) disablePoolImage(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.DisablePoolImage(request.Context(), request.PathValue("id"), request.PathValue("image_id"))
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) addPoolDevice(writer http.ResponseWriter, request *http.Request) {
	handler.setPoolDevice(writer, request, true)
}
func (handler *managementHandler) removePoolDevice(writer http.ResponseWriter, request *http.Request) {
	handler.setPoolDevice(writer, request, false)
}
func (handler *managementHandler) setPoolDevice(writer http.ResponseWriter, request *http.Request, enabled bool) {
	if !handler.available(writer, request) {
		return
	}
	var input poolDeviceInput
	if !decode(writer, request, &input) {
		return
	}
	if input.DeviceID == "" {
		writeInvalid(writer, request, "device_id is required")
		return
	}
	var err error
	if enabled {
		err = handler.service.AddDeviceToPool(request.Context(), request.PathValue("id"), input.DeviceID)
	} else {
		err = handler.service.RemoveDeviceFromPool(request.Context(), request.PathValue("id"), input.DeviceID)
	}
	handler.write(writer, request, http.StatusOK, map[string]any{"pool_id": request.PathValue("id"), "device_id": input.DeviceID, "enabled": enabled}, err)
}

func (handler *managementHandler) listDevices(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "page must be positive and page_size must be between 1 and 200")
		return
	}
	value, err := handler.service.ListDevices(request.Context(), page)
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) getDevice(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.GetDevice(request.Context(), request.PathValue("id"))
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *managementHandler) restartDevice(writer http.ResponseWriter, request *http.Request) {
	handler.deviceAction(writer, request, "restart")
}
func (handler *managementHandler) rebuildDevice(writer http.ResponseWriter, request *http.Request) {
	handler.deviceAction(writer, request, "rebuild")
}
func (handler *managementHandler) quarantineDevice(writer http.ResponseWriter, request *http.Request) {
	handler.deviceAction(writer, request, "quarantine")
}
func (handler *managementHandler) unquarantineDevice(writer http.ResponseWriter, request *http.Request) {
	handler.deviceAction(writer, request, "unquarantine")
}
func (handler *managementHandler) deviceAction(writer http.ResponseWriter, request *http.Request, action string) {
	if !handler.available(writer, request) {
		return
	}
	var input reasonInput
	if !decode(writer, request, &input) {
		return
	}
	if (action == "restart" || action == "rebuild") && !requireIdempotencyKey(writer, request) {
		return
	}
	var value management.Device
	var err error
	actor := requestActor(request)
	requestID := correlation.FromContext(request.Context()).RequestID
	switch action {
	case "restart":
		value, err = handler.service.RestartDeviceAudited(request.Context(), request.PathValue("id"), input.Reason, actor, requestID, request.Header.Get("Idempotency-Key"))
	case "rebuild":
		value, err = handler.service.RebuildDeviceAudited(request.Context(), request.PathValue("id"), input.Reason, actor, requestID, request.Header.Get("Idempotency-Key"))
	case "quarantine":
		value, err = handler.service.QuarantineDeviceAudited(request.Context(), request.PathValue("id"), input.Reason, actor, requestID)
	case "unquarantine":
		value, err = handler.service.UnquarantineDeviceAudited(request.Context(), request.PathValue("id"), input.Reason, actor, requestID)
	}
	status := http.StatusOK
	if action == "restart" || action == "rebuild" {
		status = http.StatusAccepted
	}
	handler.write(writer, request, status, value, err)
}

type reasonInput struct {
	Reason string `json:"reason"`
}
type poolDeviceInput struct {
	DeviceID string `json:"device_id"`
}

func (handler *managementHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "management service is not configured", Retryable: true})
	return false
}

func (handler *managementHandler) write(writer http.ResponseWriter, request *http.Request, status int, data any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, data)
		return
	}
	writeManagementError(writer, request, err)
}

func decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInvalid(writer, request, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalid(writer, request, "request body must contain one JSON object")
		return false
	}
	return true
}

func writeInvalid(writer http.ResponseWriter, request *http.Request, message string) {
	httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: message, Retryable: false})
}

func requireIdempotencyKey(writer http.ResponseWriter, request *http.Request) bool {
	if len(request.Header.Get("Idempotency-Key")) >= 8 {
		return true
	}
	writeInvalid(writer, request, "Idempotency-Key header must contain at least 8 characters")
	return false
}

func writeManagementError(writer http.ResponseWriter, request *http.Request, err error) {
	apiError := httpx.APIError{Code: "INTERNAL_ERROR", Message: "internal server error", Retryable: false}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, management.ErrInvalidArgument):
		status, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	case errors.Is(err, management.ErrNotFound):
		status, apiError = http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: "resource not found"}
	case errors.Is(err, management.ErrConflict):
		status, apiError = http.StatusConflict, httpx.APIError{Code: "CONFLICT", Message: err.Error()}
	case errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, management.ErrHostUnavailable), errors.Is(err, management.ErrImageUnavailable):
		status, apiError = http.StatusConflict, httpx.APIError{Code: "INVALID_STATE_TRANSITION", Message: err.Error()}
	default:
		var providerError *providers.Error
		if errors.As(err, &providerError) {
			status = http.StatusServiceUnavailable
			apiError = httpx.APIError{Code: providerError.Code, Message: providerError.Message, Retryable: providerError.Retryable}
		}
		var transitionError *domain.TransitionError
		if errors.As(err, &transitionError) {
			status = http.StatusConflict
			apiError = httpx.APIError{Code: "INVALID_STATE_TRANSITION", Message: transitionError.Error()}
		}
	}
	httpx.WriteError(writer, request, status, apiError)
}

// clientID is the idempotency scope of the caller. It is deliberately narrower
// than the audit actor: several service actors share one client namespace,
// while each console user gets its own.
func clientID(request *http.Request) string {
	return requestActor(request).ClientID
}
