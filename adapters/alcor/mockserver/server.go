package mockserver

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/adapters/alcor"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

const (
	ScenarioHappy               = "happy"
	ScenarioCapacityUnavailable = "capacity_unavailable"
	ScenarioInfrastructureFail  = "infra_failure"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type Config struct {
	Token        string
	Scenario     string
	PendingPolls int
}

type Server struct {
	mutex        sync.Mutex
	config       Config
	nextID       int
	reservations map[string]*reservationState
	createByKey  map[string]string
	operations   map[string]string
	mux          http.Handler
}

type reservationState struct {
	Reservation alcor.Reservation
	Polls       int
}

type createInput struct {
	PoolID                string         `json:"pool_id"`
	OwnerType             string         `json:"owner_type"`
	OwnerID               string         `json:"owner_id"`
	RequestedCapabilities map[string]any `json:"requested_capabilities"`
	LeaseSeconds          int            `json:"lease_seconds"`
}

func New(config Config) *Server {
	if config.Token == "" {
		config.Token = "mock-service-token"
	}
	if config.Scenario == "" {
		config.Scenario = ScenarioHappy
	}
	if config.PendingPolls <= 0 {
		config.PendingPolls = 1
	}
	server := &Server{config: config, reservations: map[string]*reservationState{}, createByKey: map[string]string{}, operations: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("POST /api/v1/device-reservations", server.createReservation)
	mux.HandleFunc("GET /api/v1/device-reservations/{id}", server.getReservation)
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/extensions", server.extendReservation)
	mux.HandleFunc("POST /api/v1/device-reservations/{id}/releases", server.releaseReservation)
	mux.HandleFunc("GET /api/v1/devices/{id}", server.getDevice)
	mux.HandleFunc("/", server.notFound)
	server.mux = correlation.Middleware(server.authenticate(mux))
	return server
}

func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mux.ServeHTTP(writer, request)
}

func (server *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" {
			next.ServeHTTP(writer, request)
			return
		}
		actual := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if len(actual) != len(server.config.Token) || subtle.ConstantTimeCompare([]byte(actual), []byte(server.config.Token)) != 1 {
			httpx.WriteError(writer, request, http.StatusUnauthorized, httpx.APIError{Code: "UNAUTHORIZED", Message: "invalid mock service token"})
			return
		}
		if !identifierPattern.MatchString(request.Header.Get(correlation.HeaderRunID)) ||
			!identifierPattern.MatchString(request.Header.Get(correlation.HeaderAttemptID)) {
			httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "Run and RunAttempt correlation headers are required"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) health(writer http.ResponseWriter, request *http.Request) {
	httpx.WriteData(writer, request, http.StatusOK, map[string]string{"status": "ok", "scenario": server.config.Scenario})
}

func (server *Server) createReservation(writer http.ResponseWriter, request *http.Request) {
	key := request.Header.Get("Idempotency-Key")
	if len(key) < 8 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "Idempotency-Key is required", false)
		return
	}
	if server.config.Scenario == ScenarioCapacityUnavailable {
		server.error(writer, request, http.StatusServiceUnavailable, alcor.CodeDeviceCapacityUnavailable, "mock device capacity is unavailable", true)
		return
	}
	if server.config.Scenario == ScenarioInfrastructureFail {
		server.error(writer, request, http.StatusServiceUnavailable, "KVM_UNAVAILABLE", "mock KVM host is unavailable", false)
		return
	}
	var input createInput
	if !decode(writer, request, &input) {
		return
	}
	if input.OwnerType != alcor.OwnerTypeRunAttempt || !identifierPattern.MatchString(input.OwnerID) ||
		input.OwnerID != request.Header.Get(correlation.HeaderAttemptID) || !identifierPattern.MatchString(input.PoolID) || input.LeaseSeconds < 60 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "mock requires a matching RunAttempt owner", false)
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()
	if id := server.createByKey[key]; id != "" {
		stored := server.reservations[id].Reservation
		if stored.OwnerID != input.OwnerID || stored.PoolID != input.PoolID || stored.LeaseSeconds != input.LeaseSeconds ||
			!reflect.DeepEqual(stored.RequestedCapabilities, input.RequestedCapabilities) {
			server.error(writer, request, http.StatusConflict, "CONFLICT", "idempotency key was reused with different input", false)
			return
		}
		httpx.WriteData(writer, request, http.StatusCreated, stored)
		return
	}
	server.nextID++
	now := time.Now().UTC()
	id := fmt.Sprintf("reservation_mock_%06d", server.nextID)
	reservation := alcor.Reservation{ID: id, PoolID: input.PoolID, OwnerType: input.OwnerType, OwnerID: input.OwnerID,
		RequestedCapabilities: input.RequestedCapabilities, LeaseSeconds: input.LeaseSeconds, Status: "pending", CreatedAt: now, UpdatedAt: now}
	server.reservations[id] = &reservationState{Reservation: reservation}
	server.createByKey[key] = id
	httpx.WriteData(writer, request, http.StatusCreated, reservation)
}

func (server *Server) getReservation(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	state := server.reservations[request.PathValue("id")]
	if state == nil {
		server.error(writer, request, http.StatusNotFound, "NOT_FOUND", "mock reservation not found", false)
		return
	}
	if state.Reservation.OwnerID != request.Header.Get(correlation.HeaderAttemptID) {
		server.error(writer, request, http.StatusForbidden, "FORBIDDEN", "RunAttempt does not own this reservation", false)
		return
	}
	if state.Reservation.Status == "pending" {
		state.Polls++
		if state.Polls >= server.config.PendingPolls {
			now := time.Now().UTC()
			expires := now.Add(time.Duration(state.Reservation.LeaseSeconds) * time.Second)
			deviceID := strings.Replace(state.Reservation.ID, "reservation", "device", 1)
			state.Reservation.Status = "active"
			state.Reservation.DeviceID = &deviceID
			state.Reservation.StartsAt = &now
			state.Reservation.ExpiresAt = &expires
			state.Reservation.UpdatedAt = now
		}
	}
	httpx.WriteData(writer, request, http.StatusOK, state.Reservation)
}

func (server *Server) extendReservation(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		AdditionalSeconds int `json:"additional_seconds"`
	}
	if !decode(writer, request, &input) {
		return
	}
	if input.AdditionalSeconds < 60 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "additional_seconds must be at least 60", false)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "Idempotency-Key is required", false)
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	state := server.reservations[request.PathValue("id")]
	if state == nil {
		server.error(writer, request, http.StatusNotFound, "NOT_FOUND", "mock reservation not found", false)
		return
	}
	if state.Reservation.OwnerID != request.Header.Get(correlation.HeaderAttemptID) {
		server.error(writer, request, http.StatusForbidden, "FORBIDDEN", "RunAttempt does not own this reservation", false)
		return
	}
	if state.Reservation.Status != "active" || state.Reservation.ExpiresAt == nil {
		server.error(writer, request, http.StatusConflict, "INVALID_STATE_TRANSITION", "only active reservations can be extended", false)
		return
	}
	op := "extend:" + state.Reservation.ID + ":" + key
	requestValue := strconv.Itoa(input.AdditionalSeconds)
	if previous := server.operations[op]; previous != "" && previous != requestValue {
		server.error(writer, request, http.StatusConflict, "CONFLICT", "idempotency key was reused with different input", false)
		return
	}
	if server.operations[op] == "" {
		expires := state.Reservation.ExpiresAt.Add(time.Duration(input.AdditionalSeconds) * time.Second)
		state.Reservation.ExpiresAt = &expires
		state.Reservation.UpdatedAt = time.Now().UTC()
		server.operations[op] = requestValue
	}
	httpx.WriteData(writer, request, http.StatusOK, state.Reservation)
}

func (server *Server) releaseReservation(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Reason string `json:"reason"`
	}
	if !decode(writer, request, &input) {
		return
	}
	if len(strings.TrimSpace(input.Reason)) < 3 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "release reason is required", false)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		server.error(writer, request, http.StatusBadRequest, "INVALID_ARGUMENT", "Idempotency-Key is required", false)
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	state := server.reservations[request.PathValue("id")]
	if state == nil {
		server.error(writer, request, http.StatusNotFound, "NOT_FOUND", "mock reservation not found", false)
		return
	}
	if state.Reservation.OwnerID != request.Header.Get(correlation.HeaderAttemptID) {
		server.error(writer, request, http.StatusForbidden, "FORBIDDEN", "RunAttempt does not own this reservation", false)
		return
	}
	op := "release:" + state.Reservation.ID + ":" + key
	requestValue := strings.TrimSpace(input.Reason)
	if previous := server.operations[op]; previous != "" && previous != requestValue {
		server.error(writer, request, http.StatusConflict, "CONFLICT", "idempotency key was reused with different input", false)
		return
	}
	server.operations[op] = requestValue
	if state.Reservation.Status == "pending" {
		now := time.Now().UTC()
		failureCode := "RESERVATION_CANCELED"
		state.Reservation.Status = "failed"
		state.Reservation.FailureCode = &failureCode
		state.Reservation.UpdatedAt = now
	} else if state.Reservation.Status == "active" {
		now := time.Now().UTC()
		state.Reservation.Status = "released"
		state.Reservation.ReleasedAt = &now
		state.Reservation.UpdatedAt = now
	}
	httpx.WriteData(writer, request, http.StatusOK, state.Reservation)
}

func (server *Server) getDevice(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	for _, state := range server.reservations {
		if state.Reservation.DeviceID == nil || *state.Reservation.DeviceID != request.PathValue("id") {
			continue
		}
		if state.Reservation.OwnerID != request.Header.Get(correlation.HeaderAttemptID) {
			server.error(writer, request, http.StatusForbidden, "FORBIDDEN", "RunAttempt does not own this device", false)
			return
		}
		adb, appium := "127.0.0.1:5555", "http://127.0.0.1:4723"
		device := alcor.Device{ID: *state.Reservation.DeviceID, Serial: "emulator-5554", ADBEndpoint: &adb,
			AppiumEndpoint: &appium, Capabilities: map[string]any{"platformName": "Android", "apiLevel": 34, "appiumUdid": "emulator-5554"}}
		httpx.WriteData(writer, request, http.StatusOK, device)
		return
	}
	server.error(writer, request, http.StatusNotFound, "NOT_FOUND", "mock device not found", false)
}

func (server *Server) notFound(writer http.ResponseWriter, request *http.Request) {
	server.error(writer, request, http.StatusNotFound, "NOT_FOUND", "mock route not found", false)
}

func (server *Server) error(writer http.ResponseWriter, request *http.Request, status int, code, message string, retryable bool) {
	httpx.WriteError(writer, request, status, httpx.APIError{Code: code, Message: message, Retryable: retryable})
}

func decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "invalid mock request body"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		httpx.WriteError(writer, request, http.StatusBadRequest, httpx.APIError{Code: "INVALID_ARGUMENT", Message: "mock request body must contain one JSON object"})
		return false
	}
	return true
}
