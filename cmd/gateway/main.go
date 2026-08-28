package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vantageedge/backend/internal/auth/apikey"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	redispkg "github.com/vantageedge/backend/internal/cache/redis"
	"github.com/vantageedge/backend/internal/gateway/configclient"
	"github.com/vantageedge/backend/internal/gateway/middleware"
	"github.com/vantageedge/backend/internal/gateway/proxy"
	"github.com/vantageedge/backend/internal/gateway/router"
	"github.com/vantageedge/backend/internal/loadbalancer"
	"github.com/vantageedge/backend/internal/observability"
	"github.com/vantageedge/backend/internal/ratelimit"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Invalid config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Observability.LogLevel, cfg.Observability.LogFormat)
	log.Info().Msg("Starting API Gateway")

	db, err := database.New(&cfg.Database, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	repos := repository.New(db)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	shutdownTracing, err := observability.InitTracing(rootCtx, "vantageedge-gateway", cfg.Observability.OTELExporterEndpoint, cfg.Observability.OTELEnabled)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize tracing; continuing without it")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to flush trace exporter on shutdown")
		}
	}()

	jwtValidator, err := authjwt.NewJWTValidator(rootCtx, cfg.Clerk.JWKSURL, cfg.Clerk.Issuer, cfg.Clerk.Audience)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize JWT validator from Clerk JWKS")
	}
	apiKeyValidator := apikey.NewValidator(repos)
	authenticator := middleware.NewAuthenticator(jwtValidator, apiKeyValidator)

	// Redis backs both rate limiting and caching in production: it's the
	// only way multiple gateway replicas share limiter/cache state. If it's
	// unreachable at startup, fall back to single-process in-memory
	// implementations rather than refusing to start — a degraded gateway
	// (correct behavior per-instance, but not correct across replicas) is
	// better than no gateway.
	var limiter ratelimit.Limiter
	var respCache *middleware.ResponseCache
	redisClient, err := redispkg.NewClient(fmt.Sprintf("redis://:%s@%s/%d", cfg.Redis.Password, cfg.Redis.Address(), cfg.Redis.DB))
	if err != nil {
		log.Warn().Err(err).Msg("Redis unavailable at startup; falling back to in-memory rate limiting and caching (not safe across multiple gateway replicas)")
		limiter = ratelimit.NewMemoryLimiter()
		respCache = middleware.NewResponseCache(middleware.NewMemoryStore())
	} else {
		defer redisClient.Close()
		limiter = ratelimit.NewRedisLimiter(redisClient)
		respCache = middleware.NewResponseCache(middleware.NewRedisStore(redisClient))
	}

	reverseProxy := proxy.NewReverseProxy(60 * time.Second)
	metrics := observability.NewMetrics("vantageedge_gateway")
	healthChecker := loadbalancer.NewHealthChecker(log, cfg.LoadBalancer.HealthCheckInterval)

	// The gateway's tenant/route/origin config comes from the control
	// plane's gRPC ConfigService, not direct Postgres reads (repos above
	// is kept only for the request-log write path).
	configClient, err := configclient.NewClient(cfg.Gateway.ControlPlaneGRPCAddr, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create control plane config client")
	}
	defer configClient.Close()
	configCache := router.NewConfigCache(router.NewGRPCSource(configClient), 5*time.Second)
	go configClient.WatchInvalidations(rootCtx, configCache.Invalidate)

	handler := router.New(cfg, repos, router.Deps{
		ConfigCache:   configCache,
		Authenticator: authenticator,
		Limiter:       limiter,
		Cache:         respCache,
		Proxy:         reverseProxy,
		Metrics:       metrics,
		HealthChecker: healthChecker,
	}, log)

	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	metricsAddr := fmt.Sprintf(":%d", cfg.Observability.MetricsPort)

	go func() {
		log.Info().Str("addr", addr).Msg("Gateway listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Gateway server failed")
		}
	}()

	if cfg.Observability.MetricsEnabled {
		go func() {
			log.Info().Str("addr", metricsAddr).Msg("Metrics server listening")
			if err := metrics.Serve(rootCtx, metricsAddr); err != nil {
				log.Error().Err(err).Msg("Metrics server failed")
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down gateway...")
	rootCancel()
	healthChecker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Gateway shutdown error")
	}

	log.Info().Msg("Gateway shutdown complete")
}
