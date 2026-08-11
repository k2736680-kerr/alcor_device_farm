package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/phoneprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

type provisioningHandler struct{ controller *warmpool.Controller }

type deviceProvisioningInput struct {
	PoolID            string         `json:"pool_id"`
	ImageID           string         `json:"image_id"`
	HardwareProfileID string         `json:"hardware_profile_id"`
	RuntimeProfile    map[string]any `json:"runtime_profile"`
}

func RegisterProvisioning(mux *http.ServeMux, controller *warmpool.Controller) {
	handler := &provisioningHandler{controller: controller}
	mux.HandleFunc("GET /api/v1/android-hardware-profiles", handler.listProfiles)
	mux.HandleFunc("POST /api/v1/device-provisionings", handler.create)
}

func (handler *provisioningHandler) listProfiles(writer http.ResponseWriter, request *http.Request) {
	query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
	items := make([]phoneprofile.Profile, 0, len(phoneprofile.Catalog))
	for _, profile := range phoneprofile.Catalog {
		if query == "" || strings.Contains(strings.ToLower(profile.Name), query) || strings.Contains(profile.ID, query) {
			items = append(items, profile)
		}
	}
	httpx.WriteData(writer, request, http.StatusOK, items)
}

func (handler *provisioningHandler) create(writer http.ResponseWriter, request *http.Request) {
	if handler.controller == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "device provisioning is not configured", Retryable: true})
		return
	}
	if !requireIdempotencyKey(writer, request) {
		return
	}
	var input deviceProvisioningInput
	if !decode(writer, request, &input) {
		return
	}
	profile, err := runtimeprofile.Parse(input.RuntimeProfile)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "invalid runtime profile"})
		return
	}
	value, err := handler.controller.Provision(request.Context(), warmpool.ProvisionInput{
		PoolID: input.PoolID, ImageID: input.ImageID, HardwareProfileID: input.HardwareProfileID, RuntimeProfile: profile,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
	if err == nil {
		httpx.WriteData(writer, request, http.StatusAccepted, value)
		return
	}
	status, apiError := http.StatusConflict, httpx.APIError{Code: "DEVICE_CAPACITY_UNAVAILABLE", Message: err.Error(), Retryable: true}
	if errors.Is(err, warmpool.ErrNoCapacity) {
		httpx.WriteError(writer, request, status, apiError)
		return
	}
	if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "unknown") {
		status, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	}
	httpx.WriteError(writer, request, status, apiError)
}
