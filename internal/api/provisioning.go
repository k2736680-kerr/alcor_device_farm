package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imagecatalog"
	"github.com/Ad-Quanta/alcor-device-farm/internal/phoneprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

type provisioningHandler struct {
	controller *warmpool.Controller
	catalog    *imagecatalog.Service
}

type deviceProvisioningInput struct {
	PoolID            string         `json:"pool_id"`
	CatalogID         string         `json:"catalog_id"`
	ImageID           string         `json:"image_id"`
	HardwareProfileID string         `json:"hardware_profile_id"`
	RuntimeProfile    map[string]any `json:"runtime_profile"`
}

func RegisterProvisioning(mux *http.ServeMux, controller *warmpool.Controller, catalogs ...*imagecatalog.Service) {
	handler := &provisioningHandler{controller: controller}
	if len(catalogs) > 0 {
		handler.catalog = catalogs[0]
	}
	mux.HandleFunc("GET /api/v1/android-hardware-profiles", handler.listProfiles)
	mux.HandleFunc("GET /api/v1/device-provisionings", handler.list)
	mux.HandleFunc("POST /api/v1/device-provisionings", handler.create)
	mux.HandleFunc("GET /api/v1/device-provisionings/{id}", handler.get)
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
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "设备创建服务尚未配置", Retryable: true})
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
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "设备运行规格无效"})
		return
	}
	if input.CatalogID != "" {
		if handler.catalog == nil {
			httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "安卓系统镜像目录尚未配置", Retryable: true})
			return
		}
		actor := requestActor(request)
		value, created, err := handler.controller.CreateCatalogProvisioning(request.Context(), warmpool.CatalogProvisionInput{
			ClientID: actor.ClientID, IdempotencyKey: request.Header.Get("Idempotency-Key"), PoolID: input.PoolID, CatalogID: input.CatalogID,
			HardwareProfileID: input.HardwareProfileID, RuntimeProfile: profile,
		})
		if err == nil && created {
			cached, cacheErr := handler.controller.AttachCachedPreparation(request.Context(), value.ID)
			if cacheErr != nil {
				err = cacheErr
			}
			if err == nil && !cached {
				preparation, prepareErr := handler.catalog.Prepare(request.Context(), actor, "provisioning-"+value.ID, imagecatalog.PreparationInput{CatalogID: input.CatalogID, RuntimeProfile: profile.Map()})
				if prepareErr != nil {
					err = prepareErr
				} else {
					err = handler.controller.AttachPreparation(request.Context(), value.ID, preparation.ID)
				}
			}
			if err != nil {
				// The durable job is the source of truth after this point. Persist a
				// terminal reason instead of leaving a browser-created orphan behind.
				_ = handler.controller.FailCatalogProvisioning(request.Context(), value.ID, "prepare_system_image", "PREPARATION_SCHEDULE_FAILED")
			}
		}
		if err == nil {
			httpx.WriteData(writer, request, http.StatusAccepted, value)
			return
		}
		status, apiError := http.StatusConflict, httpx.APIError{Code: "DEVICE_PROVISIONING_CONFLICT", Message: err.Error(), Retryable: true}
		if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "unknown") {
			status, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
		}
		httpx.WriteError(writer, request, status, apiError)
		return
	}
	if input.ImageID == "" {
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "必须选择系统镜像目录项或已验证镜像"})
		return
	}
	value, err := handler.controller.Provision(request.Context(), warmpool.ProvisionInput{PoolID: input.PoolID, ImageID: input.ImageID, HardwareProfileID: input.HardwareProfileID, RuntimeProfile: profile, IdempotencyKey: request.Header.Get("Idempotency-Key")})
	if err == nil {
		httpx.WriteData(writer, request, http.StatusAccepted, value)
		return
	}
	status, apiError := http.StatusConflict, httpx.APIError{Code: "DEVICE_CAPACITY_UNAVAILABLE", Message: err.Error(), Retryable: true}
	if errors.Is(err, warmpool.ErrNoCapacity) {
		var capacityError *warmpool.CapacityUnavailableError
		if errors.As(err, &capacityError) {
			apiError.Details = capacityError.Result
		}
		httpx.WriteError(writer, request, status, apiError)
		return
	}
	if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "unknown") {
		status, apiError = http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	}
	httpx.WriteError(writer, request, status, apiError)
}

func (handler *provisioningHandler) list(writer http.ResponseWriter, request *http.Request) {
	if handler.controller == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "设备创建服务尚未配置", Retryable: true})
		return
	}
	page, ok := pagination(request)
	if !ok {
		writeInvalid(writer, request, "页码必须大于零，每页数量必须在 1 到 200 之间")
		return
	}
	value, err := handler.controller.ListCatalogProvisionings(request.Context(), page)
	if err != nil {
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL", Message: "暂时无法读取设备创建任务", Retryable: true})
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, value)
}

func (handler *provisioningHandler) get(writer http.ResponseWriter, request *http.Request) {
	if handler.controller == nil {
		httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "设备创建服务尚未配置", Retryable: true})
		return
	}
	value, err := handler.controller.GetCatalogProvisioning(request.Context(), request.PathValue("id"))
	if err == nil {
		httpx.WriteData(writer, request, http.StatusOK, value)
		return
	}
	httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: "未找到设备创建任务"})
}
