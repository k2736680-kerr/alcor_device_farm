package server

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/adapters/stf"
	"github.com/Ad-Quanta/alcor-device-farm/internal/api"
	"github.com/Ad-Quanta/alcor-device-farm/internal/auth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consoleauth"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consolequery"
	"github.com/Ad-Quanta/alcor-device-farm/internal/consoleui"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/hostcommand"
	"github.com/Ad-Quanta/alcor-device-farm/internal/httpx"
	"github.com/Ad-Quanta/alcor-device-farm/internal/imagecatalog"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossession"
	"github.com/Ad-Quanta/alcor-device-farm/internal/iossimulator"
	"github.com/Ad-Quanta/alcor-device-farm/internal/management"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	farmmetrics "github.com/Ad-Quanta/alcor-device-farm/internal/metrics"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reaper"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reconcile"
	"github.com/Ad-Quanta/alcor-device-farm/internal/remotecontrol"
	"github.com/Ad-Quanta/alcor-device-farm/internal/reservation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/scheduler"
	"github.com/Ad-Quanta/alcor-device-farm/internal/warmpool"
)

type Services struct {
	Management    *management.Service
	Reservations  *reservation.Service
	Scheduler     *scheduler.Scheduler
	Reconcile     *reconcile.Service
	HostCommands  *hostcommand.Service
	Metrics       *farmmetrics.Registry
	ConsoleAuth   *consoleauth.Service
	ConsoleQuery  *consolequery.Service
	RemoteControl *remotecontrol.Service
	ImageCatalog  *imagecatalog.Service
	IOSSessions   *iossession.Service
	IOSSimulators *iossimulator.Service
	WarmPool      *warmpool.Controller
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
	var services Services
	if len(serviceSets) > 0 {
		services = serviceSets[0]
	}
	if services.Metrics == nil {
		services.Metrics = farmmetrics.New(nil)
	}
	mux.HandleFunc("/readyz", readinessHandler(services.Metrics))
	mux.Handle("/metrics", services.Metrics)
	api.RegisterManagement(mux, services.Management)
	api.RegisterImageCatalog(mux, services.ImageCatalog)
	api.RegisterProvisioning(mux, services.WarmPool, services.ImageCatalog)
	api.RegisterReservations(mux, services.Reservations)
	api.RegisterIOSSessions(mux, services.IOSSessions)
	api.RegisterIOSSimulators(mux, services.IOSSimulators)
	api.RegisterHealth(mux, services.Reconcile)
	api.RegisterHostCommands(mux, services.HostCommands)
	api.RegisterConsole(mux, services.ConsoleAuth, services.ConsoleQuery, services.RemoteControl)
	remotecontrol.RegisterGateway(mux, services.RemoteControl, logger)
	mux.Handle("/console/", consoleui.Handler())
	mux.Handle("/console", consoleui.Handler())
	mux.HandleFunc("/", notFoundHandler)

	protected := auth.RouteMiddleware(security, mux, services.ConsoleAuth)
	return correlation.Middleware(requestLogMiddleware(logger, services.Metrics, recoverMiddleware(logger, protected)))
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
		services.Metrics = farmmetrics.New(db)
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
		services.Management = management.NewService(managementpostgres.New(db), nil, nil)
		services.ImageCatalog = imagecatalog.New(db)
		if stfClient != nil {
			services.Reservations = reservation.NewService(db, nil, stfClient)
			services.Scheduler = scheduler.New(db, nil, logger, stfClient)
		} else {
			services.Reservations = reservation.NewService(db, nil)
			services.Scheduler = scheduler.New(db, nil, logger)
		}
		services.IOSSessions = iossession.New(db, cfg.Security.AgentToken)
		services.IOSSimulators = iossimulator.New(db, nil)
		services.Reservations.SetIOSSessionController(services.IOSSessions)
		go services.IOSSessions.RunReconcile(ctx, cfg.Reconcile.Interval, logger)
		go services.Scheduler.Run(ctx, cfg.Lease.SchedulerInterval)
		reservationReaper := reaper.New(services.Reservations, cfg.Lease.GracePeriod, logger)
		go reservationReaper.Run(ctx, cfg.Lease.ReaperInterval)
		var visibility reconcile.Visibility
		if stfClient != nil {
			visibility = stfClient
		}
		// Real Provider operations belong to the Host Agent. The Server reconciles
		// database state and Agent heartbeat observations without Docker access.
		services.Reconcile = reconcile.New(db, nil, visibility, cfg.Reconcile.FailureThreshold,
			cfg.Reconcile.STFVisibilityGrace, cfg.Reconcile.HostRecoveryGrace, logger)
		go services.Reconcile.Run(ctx, cfg.Reconcile.Interval, cfg.Reconcile.HostTimeout)
		services.HostCommands = hostcommand.New(db)
		go services.HostCommands.RunLeaseRecovery(ctx, time.Second)
		services.WarmPool = warmpool.New(db, nil, logger)
		go services.WarmPool.Run(ctx, cfg.WarmPool.Interval)
		services.ConsoleQuery = consolequery.New(db)
		if cfg.Console.Enabled {
			users, loadErr := consoleauth.LoadUsers(cfg.Console.UsersFile)
			if loadErr != nil {
				return loadErr
			}
			services.ConsoleAuth, err = consoleauth.New(db, cfg.Console, users)
			if err != nil {
				return err
			}
			go services.ConsoleAuth.RunCleanup(ctx)
			if (stfClient != nil && cfg.STF.WebConfigured()) || cfg.IOSRemote.Configured() {
				services.RemoteControl, err = remotecontrol.New(
					services.Reservations, services.Management, remotecontrol.ConfigFrom(cfg), services.IOSSessions,
				)
				if err != nil {
					return err
				}
			}
		}
	}
	if services.Metrics == nil {
		services.Metrics = farmmetrics.New(nil)
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

func readinessHandler(registry *farmmetrics.Registry) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, request)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := registry.Ready(ctx); err != nil {
			httpx.WriteError(writer, request, http.StatusServiceUnavailable, httpx.APIError{
				Code: "SERVICE_UNAVAILABLE", Message: "数据库尚未就绪", Retryable: true,
			})
			return
		}
		httpx.WriteData(writer, request, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func notFoundHandler(writer http.ResponseWriter, request *http.Request) {
	httpx.WriteError(writer, request, http.StatusNotFound, httpx.APIError{
		Code:      "NOT_FOUND",
		Message:   "未找到指定资源",
		Retryable: false,
	})
}

func methodNotAllowed(writer http.ResponseWriter, request *http.Request) {
	httpx.WriteError(writer, request, http.StatusMethodNotAllowed, httpx.APIError{
		Code:      "METHOD_NOT_ALLOWED",
		Message:   "不支持当前请求方法",
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
					Message:   "服务器内部错误",
					Retryable: false,
				})
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func requestLogMiddleware(logger *slog.Logger, registry *farmmetrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		statusWriter := &responseStatusWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(statusWriter, request)
		registry.ObserveHTTP(request.Method, request.Pattern, statusWriter.status, time.Since(startedAt))

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

func (writer *responseStatusWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *responseStatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	connection, buffered, err := hijacker.Hijack()
	if err == nil {
		writer.status = http.StatusSwitchingProtocols
		writer.wroteHeader = true
	}
	return connection, buffered, err
}

func (writer *responseStatusWriter) Flush() {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}
