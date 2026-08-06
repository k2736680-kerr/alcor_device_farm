package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
)

type Envelope struct {
	RequestID string    `json:"request_id"`
	Data      any       `json:"data"`
	Error     *APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	Retryable bool   `json:"retryable"`
}

func WriteData(writer http.ResponseWriter, request *http.Request, status int, data any) {
	writeJSON(writer, status, Envelope{
		RequestID: correlation.FromContext(request.Context()).RequestID,
		Data:      data,
		Error:     nil,
	})
}

func WriteError(writer http.ResponseWriter, request *http.Request, status int, apiError APIError) {
	writeJSON(writer, status, Envelope{
		RequestID: correlation.FromContext(request.Context()).RequestID,
		Data:      nil,
		Error:     &apiError,
	})
}

func writeJSON(writer http.ResponseWriter, status int, value Envelope) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
