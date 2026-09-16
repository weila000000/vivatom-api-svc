package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vivatom-api-svc/internal/agent"
	"vivatom-api-svc/internal/ai"
	"vivatom-api-svc/internal/appconfig"
	"vivatom-api-svc/internal/audit"
	"vivatom-api-svc/internal/catalog"
	"vivatom-api-svc/internal/compiler"
	"vivatom-api-svc/internal/generation"
	"vivatom-api-svc/internal/httpapi"
	"vivatom-api-svc/internal/identity"
	"vivatom-api-svc/internal/platform/sqlite"
	runtimeservice "vivatom-api-svc/internal/runtime"
	"vivatom-api-svc/internal/team"
	"vivatom-api-svc/internal/usage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	config, err := appconfig.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	database, err := sqlite.Open(config.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	aiConfig, err := ai.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	provider := ai.NewProvider(aiConfig)
	logger.Info("agent_provider_configured", "mode", aiConfig.Mode, "analyst_model", aiConfig.AnalystModel, "architect_model", aiConfig.ArchitectModel, "builder_model", aiConfig.BuilderModel)
	orchestrator := agent.NewOrchestrator(provider, generation.NewGuard())
	runtimeService := runtimeservice.NewService(sqlite.NewRuntimeRepository(database))
	identityService := identity.NewService(sqlite.NewIdentityRepository(database))
	catalogService := catalog.NewService(identityService, sqlite.NewCatalogRepository(database))
	teamService := team.NewService(identityService, sqlite.NewTeamRepository(database))
	auditService := audit.NewService(identityService, sqlite.NewAuditRepository(database))
	buildCompiler := compiler.NewClient(config.BuilderURL, config.BuilderToken, config.BuilderTimeout)
	usageRepository := sqlite.NewUsageRepository(database)
	recovered, err := usageRepository.RecoverInterrupted(context.Background(), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		log.Fatal(err)
	}
	if recovered > 0 {
		logger.Warn("interrupted_agent_runs_recovered", "count", recovered)
	}
	usageService := usage.NewServiceWithCompiler(identityService, orchestrator, usageRepository, buildCompiler, config.BuilderTimeout)
	databaseReady := sqlite.ReadyCheck(database)
	router := httpapi.NewRouter(httpapi.Dependencies{
		DirectAgent: orchestrator,
		Ready: func() error {
			if err := databaseReady(); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return buildCompiler.Ready(ctx)
		},
		AgentRunner:     usageService,
		Usage:           usageService,
		Runtime:         runtimeService,
		Identity:        identityService,
		Catalog:         catalogService,
		Team:            teamService,
		Audit:           auditService,
		Logger:          logger,
		AllowedOrigin:   config.AllowedOrigin,
		AuthRateLimit:   config.AuthRateLimit,
		AgentRateLimit:  config.AgentRateLimit,
		RateLimitWindow: config.RateLimitWindow,
		TrustedProxies:  config.TrustedProxies,
	})
	server := &http.Server{
		Addr:         config.Address,
		Handler:      router,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		IdleTimeout:  config.IdleTimeout,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http_server_started", "address", config.Address)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case received := <-signals:
		logger.Info("shutdown_started", "signal", received.String())
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		return
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown_failed", "error", err)
		return
	}
	logger.Info("shutdown_complete")
}
