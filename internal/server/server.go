package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/api"
	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	providermock "github.com/Ad-Quanta/alcor-device-farm/internal/providers/mock"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reaper"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

type Services struct {
	Management   *management.Service
	Reservations *reservation.Service
	Scheduler    *scheduler.Scheduler
	Reconcile    *reconcile.Service
	HostCommands *hostcommand.Service
}

func NewHTTPServer(cfg config.Config, logger *slog.Logger, services Services) *http.Server {
	return &http.Server{
		Addr:         cfg.Server.Address,
		Handler:      Handler(cfg.Security, logger, services),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
}

func Handler(security config.SecurityConfig, logger *slog.Logger, serviceSets ...Services) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readyHandler)
	var services Services
	if len(serviceSets) > 0 {
		services = serviceSets[0]
	}
	api.RegisterManagement(mux, services.Management)
	api.RegisterReservations(mux, services.Reservations)
	api.RegisterHealth(mux, services.Reconcile)
	api.RegisterHostCommands(mux, services.HostCommands)
	mux.HandleFunc("/", notFoundHandler)

	protected := auth.RouteMiddleware(security, mux)
	return correlation.Middleware(recoverMiddleware(logger, requestLogMiddleware(logger, protected)))
}

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var services Services
	var db *database.DB
	if cfg.Database.URL != "" {
		var err error
		db, err = database.Open(ctx, cfg.Database.URL)
		if err != nil {
			return err
		}
		defer db.Close()
		provider := providermock.New(providermock.Config{})
		var stfClient *stf.Client
		if cfg.STF.Enabled {
			stfClient, err = stf.New(stf.Config{
				BaseURL: cfg.STF.BaseURL, Token: cfg.STF.APIToken, Timeout: cfg.STF.Timeout,
				Attempts: cfg.STF.Attempts, RetryDelay: cfg.STF.RetryDelay,
			})
			if err != nil {
				return err
			}
		}
		services.Management = management.NewService(managementpostgres.New(db), provider, nil)
		if stfClient != nil {
			services.Reservations = reservation.NewService(db, nil, stfClient)
			services.Scheduler = scheduler.New(db, nil, logger, stfClient)
		} else {
			services.Reservations = reservation.NewService(db, nil)
			services.Scheduler = scheduler.New(db, nil, logger)
		}
		go services.Scheduler.Run(ctx, cfg.Lease.SchedulerInterval)
		reservationReaper := reaper.New(services.Reservations, cfg.Lease.GracePeriod, logger)
		go reservationReaper.Run(ctx, cfg.Lease.ReaperInterval)
		var visibility reconcile.Visibility
		if stfClient != nil {
			visibility = stfClient
		}
		services.Reconcile = reconcile.New(db, provider, visibility, cfg.Reconcile.FailureThreshold, logger)
		go services.Reconcile.Run(ctx, cfg.Reconcile.Interval, cfg.Reconcile.HostTimeout)
		services.HostCommands = hostcommand.New(db)
		go services.HostCommands.RunLeaseRecovery(ctx, time.Second)
		warmPoolController := warmpool.New(db, nil, logger)
		go warmPoolController.Run(ctx, cfg.WarmPool.Interval)
	}
	httpServer := NewHTTPServer(cfg, logger, services)
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
