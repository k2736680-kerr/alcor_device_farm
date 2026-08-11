package api

import (
	"errors"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imagecatalog"
)

type imageCatalogHandler struct{ service *imagecatalog.Service }

func RegisterImageCatalog(mux *http.ServeMux, service *imagecatalog.Service) {
	handler := &imageCatalogHandler{service: service}
	mux.HandleFunc("GET /api/v1/android-system-images", handler.list)
	mux.HandleFunc("POST /api/v1/android-system-images/synchronizations", handler.sync)
	mux.HandleFunc("POST /api/v1/android-system-images/preparations", handler.prepare)
}

func (handler *imageCatalogHandler) available(writer http.ResponseWriter, request *http.Request) bool {
	if handler.service != nil {
		return true
	}
	httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{Code: "SERVICE_UNAVAILABLE", Message: "Android image catalog is not configured", Retryable: true})
	return false
}
func (handler *imageCatalogHandler) list(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) {
		return
	}
	value, err := handler.service.List(request.Context())
	handler.write(writer, request, http.StatusOK, value, err)
}
func (handler *imageCatalogHandler) sync(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	value, err := handler.service.Sync(request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"))
	handler.write(writer, request, http.StatusAccepted, value, err)
}
func (handler *imageCatalogHandler) prepare(writer http.ResponseWriter, request *http.Request) {
	if !handler.available(writer, request) || !requireIdempotencyKey(writer, request) {
		return
	}
	var input imagecatalog.PreparationInput
	if !decode(writer, request, &input) {
		return
	}
	value, err := handler.service.Prepare(request.Context(), requestActor(request), request.Header.Get("Idempotency-Key"), input)
	handler.write(writer, request, http.StatusAccepted, value, err)
}
func (handler *imageCatalogHandler) write(writer http.ResponseWriter, request *http.Request, status int, value any, err error) {
	if err == nil {
		httpx.WriteData(writer, request, status, value)
		return
	}
	switch {
	case errors.Is(err, imagecatalog.ErrInvalidArgument):
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: err.Error()})
	case errors.Is(err, imagecatalog.ErrNotFound):
		httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{Code: "NOT_FOUND", Message: err.Error()})
	case errors.Is(err, imagecatalog.ErrNoBuildAgent):
		httpx.WriteError(writer, request, http.StatusConflict, httpx.APIError{Code: "BUILD_AGENT_UNAVAILABLE", Message: err.Error(), Retryable: true})
	case errors.Is(err, imagecatalog.ErrConflict):
		httpx.WriteError(writer, request, http.StatusConflict, httpx.APIError{Code: "CONFLICT", Message: err.Error()})
	default:
		httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{Code: "INTERNAL_ERROR", Message: "internal server error"})
	}
}
