package main

import (
	"path/filepath"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestBackupSQLiteIncludesCommittedWALData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	if err := database.Create(&models.Drama{Title: "backed up", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	backup := path + ".backup"
	if err := backupSQLite(database, backup); err != nil {
		t.Fatal(err)
	}
	copyDB, err := db.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := copyDB.Model(&models.Drama{}).Where("title = ?", "backed up").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backup row count=%d", count)
	}
}
