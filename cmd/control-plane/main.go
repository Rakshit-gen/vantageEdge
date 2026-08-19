package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/vantageedge/backend/internal/auth/clerk"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	"github.com/vantageedge/backend/internal/controlplane/handlers"
	cpmiddleware "github.com/vantageedge/backend/internal/controlplane/middleware"
	"github.com/vantageedge/backend/internal/controlplane/service"
	"github.com/vantageedge/backend/internal/eventbus"
	"github.com/vantageedge/backend/internal/observability"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"

	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/internal/controlplane/grpcserver"
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
	log.Info().Msg("Starting Control Plane service")

	db, err := database.New(&cfg.Database, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	repos := repository.New(db)
	clerkClient := clerk.NewClerkClient(cfg.Clerk.SecretKey, cfg.Clerk.APIURL)
	hub := eventbus.NewHub()
	svc := service.New(repos, clerkClient, hub, log)
	h := handlers.New(svc, log)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	shutdownTracing, err := observability.InitTracing(rootCtx, "vantageedge-controlplane", cfg.Observability.OTELExporterEndpoint, cfg.Observability.OTELEnabled)
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

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORS.AllowedOrigins,
		AllowedMethods:   cfg.CORS.AllowedMethods,
		AllowedHeaders:   cfg.CORS.AllowedHeaders,
		AllowCredentials: cfg.CORS.AllowCredentials,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(cpmiddleware.RequireAuth(jwtValidator, svc.Auth, log))
		h.RegisterRoutes(r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.ControlPlane.Host, cfg.ControlPlane.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: otelhttp.NewHandler(r, "control-plane"),
	}

	metrics := observability.NewMetrics("vantageedge_controlplane")
	metricsAddr := fmt.Sprintf(":%d", cfg.Observability.MetricsPort)

	// gRPC ConfigService: what the gateway reads tenant/route/origin
	// config from instead of hitting Postgres directly. Route/origin
	// mutations publish to hub (see internal/controlplane/service), which
	// StreamConfigUpdates forwards to any gateway subscribed for a given
	// tenant's invalidations.
	grpcAddr := fmt.Sprintf("%s:%d", cfg.ControlPlane.Host, cfg.ControlPlane.GRPCPort)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to listen for gRPC")
	}
	grpcServer := grpc.NewServer()
	configpb.RegisterConfigServiceServer(grpcServer, grpcserver.NewConfigServer(repos, hub, log))

	go func() {
		log.Info().Str("addr", addr).Msg("HTTP server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	go func() {
		log.Info().Str("addr", grpcAddr).Msg("gRPC ConfigService listening")
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Error().Err(err).Msg("gRPC server failed")
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

	log.Info().Msg("Shutting down gracefully...")
	rootCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}
	grpcServer.GracefulStop()

	log.Info().Msg("Shutdown complete")
}
