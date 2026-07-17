package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/services/mediamigrate"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/gorm"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "configuration file")
	apply := flag.Bool("apply", false, "download and update records")
	backupConfirmed := flag.Bool("backup-confirmed", false, "confirm that the database has been backed up")
	limit := flag.Int("limit", 1000, "maximum records")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	database, err := db.OpenDatabase(cfg.Database.Type, cfg.Database.Path, cfg.Database.DSN)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		log.Fatal(err)
	}
	service := &mediamigrate.Service{DB: database, Store: storage.NewLocal(cfg.Storage.LocalPath)}
	candidates, err := service.Scan(*limit)
	if err != nil {
		log.Fatal(err)
	}
	for _, candidate := range candidates {
		fmt.Printf("%s:%d %s\n", candidate.TargetType, candidate.TargetID, candidate.SourceURL)
	}
	if !*apply {
		fmt.Printf("dry-run: %d external media records; rerun with --apply --backup-confirmed\n", len(candidates))
		return
	}
	if !*backupConfirmed {
		log.Fatal("--backup-confirmed is required with --apply")
	}
	if cfg.Database.Type == "sqlite" || cfg.Database.Type == "" {
		if _, err := os.Stat(cfg.Database.Path); err == nil {
			backup := cfg.Database.Path + ".pre-media-migration"
			if backupErr := backupSQLite(database, backup); backupErr != nil {
				log.Fatal(backupErr)
			}
			absolute, _ := filepath.Abs(backup)
			fmt.Printf("backup: %s\n", absolute)
		}
	}
	result := service.Run(context.Background(), candidates)
	fmt.Printf("found=%d succeeded=%d failed=%d\n", result.Found, result.Succeeded, result.Failed)
	if result.Failed > 0 {
		os.Exit(1)
	}
}

func backupSQLite(database *gorm.DB, destination string) error {
	if database == nil {
		return fmt.Errorf("database is required")
	}
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("backup already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return database.Exec("VACUUM INTO ?", destination).Error
}
