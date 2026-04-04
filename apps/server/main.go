package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/shaik-zeeshan/open-sandbox/docs"
	"github.com/shaik-zeeshan/open-sandbox/internal/api"
	"github.com/shaik-zeeshan/open-sandbox/internal/docker"
	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

// @title Open Sandbox API
// @version 1.0
// @description Open Sandbox manages local Docker-based agentic coding environments.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	_ = godotenv.Load(".env")
	_ = godotenv.Load("apps/server/.env")

	cli, err := docker.NewClient()
	if err != nil {
		log.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()

	authConfig, err := api.LoadAuthConfigFromEnv()
	if err != nil {
		log.Fatalf("failed to load auth config: %v", err)
	}

	sandboxStore, err := store.OpenSQLite(loadEnv("SANDBOX_DB_PATH", "open-sandbox.db"))
	if err != nil {
		log.Fatalf("failed to initialize sandbox store: %v", err)
	}
	defer sandboxStore.Close()

	server := api.NewServerWithStore(cli, authConfig, sandboxStore)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Router(),
		ReadHeaderTimeout: loadServerDuration("SANDBOX_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       loadServerDuration("SANDBOX_HTTP_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:      loadServerDuration("SANDBOX_HTTP_WRITE_TIMEOUT", 5*time.Minute),
		IdleTimeout:       loadServerDuration("SANDBOX_HTTP_IDLE_TIMEOUT", 2*time.Minute),
	}

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited: %v", err)
	}
}

func loadEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func loadServerDuration(envKey string, fallback time.Duration) time.Duration {
	raw := os.Getenv(envKey)
	if raw == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 0 {
		return fallback
	}

	return parsed
}
