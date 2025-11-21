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
	"github.com/vantageedge/backend/internal/controlplane/handlers"
	"github.com/vantageedge/backend/internal/controlplane/service"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
	"google.golang.org/grpc"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New(cfg.Observability.LogLevel, cfg.Observability.LogFormat)
	log.Info().Msg("Starting Control Plane service")

	// Connect to database
	db, err := database.New(&cfg.Database, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	// Initialize repositories
	repos := repository.New(db)

	// Initialize services
	svc := service.New(repos, log)

	// Initialize HTTP handlers
	h := handlers.New(svc, log)

	// Setup HTTP router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORS.AllowedOrigins,
		AllowedMethods:   cfg.CORS.AllowedMethods,
		AllowedHeaders:   cfg.CORS.AllowedHeaders,
		AllowCredentials: cfg.CORS.AllowCredentials,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		h.RegisterRoutes(r)
	})

	// HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.ControlPlane.Host, cfg.ControlPlane.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// gRPC server (placeholder)
	grpcAddr := fmt.Sprintf("%s:%d", cfg.ControlPlane.Host, cfg.ControlPlane.GRPCPort)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to listen for gRPC")
	}
	grpcServer := grpc.NewServer()

	// Start servers
	go func() {
		log.Info().Str("addr", addr).Msg("HTTP server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	go func() {
		log.Info().Str("addr", grpcAddr).Msg("gRPC server listening")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC server failed")
		}
	}()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}

	grpcServer.GracefulStop()
	log.Info().Msg("Shutdown complete")
}

