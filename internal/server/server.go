package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
)

func NewHTTPServer(cfg config.Config, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      Handler(logger),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
}

func Handler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readyHandler)
	mux.HandleFunc("/", notFoundHandler)

	return correlation.Middleware(recoverMiddleware(logger, requestLogMiddleware(logger, mux)))
}

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	httpServer := NewHTTPServer(cfg, logger)
	errorChannel := make(chan error, 1)

	go func() {
		logger.Info("device farm server listening", "address", cfg.Server.Address)
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorChannel <- err
			return
		}
		errorChannel <- nil
	}()

	select {
	case err := <-errorChannel:
		return err
	case <-ctx.Done():
		logger.Info("device farm server shutting down")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return err
	}
	return <-errorChannel
}

func healthHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, request)
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, map[string]string{"status": "ok"})
}

func readyHandler(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, request)
		return
	}
	httpx.WriteData(writer, request, http.StatusOK, map[string]string{"status": "ready"})
}

func notFoundHandler(writer http.ResponseWriter, request *http.Request) {
	httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{
		Code:      "NOT_FOUND",
		Message:   "resource not found",
		Retryable: false,
	})
}

func methodNotAllowed(writer http.ResponseWriter, request *http.Request) {
	httpx.WriteError(writer, request, http.StatusMethodNotAllowed, httpx.APIError{
		Code:      "METHOD_NOT_ALLOWED",
		Message:   "method not allowed",
		Retryable: false,
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				attrs := correlation.LogAttrs(request.Context())
				attrs = append(attrs, "panic", recovered, "stack", string(debug.Stack()))
				logger.Error("http handler panic", attrs...)
				httpx.WriteError(writer, request, http.StatusInternalServerError, httpx.APIError{
					Code:      "INTERNAL_ERROR",
					Message:   "internal server error",
					Retryable: false,
				})
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func requestLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		statusWriter := &responseStatusWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(statusWriter, request)

		attrs := correlation.LogAttrs(request.Context())
		attrs = append(attrs,
			"method", request.Method,
			"path", request.URL.Path,
			"status", statusWriter.status,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		logger.Info("http request completed", attrs...)
	})
}

type responseStatusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (writer *responseStatusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseStatusWriter) Write(content []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(content)
}
