package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/httpapi"
	"github.com/eqzhou/flyaimovie/internal/security"
	"github.com/eqzhou/flyaimovie/internal/services/generation"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/production"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func main() {
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			log.Fatalf("required media tool %s was not found in PATH", binary)
		}
	}
	root := findProjectRoot()
	cfgPath := envOr("CONFIG_PATH", filepath.Join(root, "configs", "config.yaml"))
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		if err := cfg.ValidateProduction(); err != nil {
			log.Fatalf("production config: %v", err)
		}
	}

	// Resolve relative paths against project root
	if !filepath.IsAbs(cfg.Database.Path) {
		cfg.Database.Path = filepath.Join(root, cfg.Database.Path)
	}
	if !filepath.IsAbs(cfg.Storage.LocalPath) {
		cfg.Storage.LocalPath = filepath.Join(root, cfg.Storage.LocalPath)
	}

	gdb, err := db.OpenDatabase(cfg.Database.Type, cfg.Database.Path, cfg.Database.DSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	if err := db.AutoMigrate(gdb); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := db.SeedDefaults(gdb); err != nil {
		log.Fatalf("seed: %v", err)
	}
	if err := security.MigrateAIConfigSecrets(gdb); err != nil {
		log.Fatalf("protect AI credentials: %v", err)
	}

	store := storage.NewLocal(cfg.Storage.LocalPath)
	skillsDir := filepath.Join(root, "backend", "skills")
	if _, err := os.Stat(skillsDir); err != nil {
		skillsDir = filepath.Join(root, "skills")
	}
	frontendDist := filepath.Join(root, "frontend", "dist")
	if _, err := os.Stat(frontendDist); err != nil {
		frontendDist = ""
	}

	srv := httpapi.NewServer(cfg, store, skillsDir, frontendDist)
	// background poller for async image/video jobs
	async := &generation.AsyncRunner{Images: srv.Images, Videos: srv.Videos, TTS: srv.TTS, Jobs: jobs.New(gdb), Store: store, Cache: srv.Cache}
	async.Start()
	productionWorker := &production.Worker{Service: srv.Productions}
	productionWorker.Start()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	if p := os.Getenv("PORT"); p != "" {
		addr = cfg.Server.Host + ":" + p
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: durationOr(cfg.Server.ReadTimeout, 30*time.Second),
		ReadTimeout:       durationOr(cfg.Server.ReadTimeout, 10*time.Minute),
		WriteTimeout:      durationOr(cfg.Server.WriteTimeout, 10*time.Minute),
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	shutdownCtx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()
	go func() {
		log.Printf("FlyAiMovie API listening on http://%s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server: %v", err)
		}
	}()
	<-shutdownCtx.Done()
	async.Stop()
	productionWorker.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}

func durationOr(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func findProjectRoot() string {
	// Prefer cwd when configs/ exists
	wd, _ := os.Getwd()
	candidates := []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "configs")); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return wd
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
