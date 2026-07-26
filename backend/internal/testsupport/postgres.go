// Package testsupport gives tests a database that matches production.
//
// The suite used to run on SQLite while production runs on PostgreSQL. That
// gap hid a real defect: GORM omits a zero-value field from the INSERT when
// the field carries a `default` tag, so `is_active:false` silently persisted
// as true. Every test passed because SQLite and PostgreSQL disagree on column
// defaults, type coercion and transaction behaviour.
//
// Each caller gets its own throwaway database on the local PostgreSQL server,
// migrated and dropped automatically, so tests stay isolated and parallel-safe
// while exercising the engine the product actually ships on.
package testsupport

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"gorm.io/gorm"
)

const databasePrefix = "pgtest_flyaimovie_"

var (
	adminOnce sync.Once
	adminDB   *gorm.DB
	adminErr  error
)

// Config describes how to reach the local PostgreSQL server. Every field can
// be overridden so CI or a non-default install does not require code changes.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	AdminDB  string
}

func loadConfig() Config {
	return Config{
		Host:     envOr("PGTESTDB_HOST", "127.0.0.1"),
		Port:     envOr("PGTESTDB_PORT", "5432"),
		User:     envOr("PGTESTDB_USER", os.Getenv("USER")),
		Password: os.Getenv("PGTESTDB_PASSWORD"),
		AdminDB:  envOr("PGTESTDB_SUPERDB", "postgres"),
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (c Config) dsn(database string) string {
	dsn := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable", c.Host, c.Port, c.User, database)
	if c.Password != "" {
		dsn += " password=" + c.Password
	}
	return dsn
}

// admin opens a single shared connection to the maintenance database. Creating
// and dropping databases cannot happen from inside the database being changed.
func admin(cfg Config) (*gorm.DB, error) {
	adminOnce.Do(func() {
		adminDB, adminErr = db.OpenDatabase("postgres", "", cfg.dsn(cfg.AdminDB))
		if adminErr != nil {
			adminErr = fmt.Errorf("connect to PostgreSQL at %s:%s as %s: %w\n"+
				"Tests require a local PostgreSQL server. Start it with `brew services start postgresql@18`, "+
				"or point PGTESTDB_HOST/PGTESTDB_PORT/PGTESTDB_USER at another server.",
				cfg.Host, cfg.Port, cfg.User, adminErr)
		}
	})
	return adminDB, adminErr
}

// OpenDatabase creates a migrated, empty PostgreSQL database for the test and
// registers cleanup that drops it. The returned handle is also assigned to
// db.DB, matching how the application wires its global handle.
func OpenDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	cfg := loadConfig()
	control, err := admin(cfg)
	if err != nil {
		t.Fatalf("testsupport: %v", err)
	}

	name := uniqueName(t.Name())
	if err := control.Exec(fmt.Sprintf(`CREATE DATABASE %q`, name)).Error; err != nil {
		t.Fatalf("testsupport: create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		// FORCE detaches lingering sessions; a leaked connection would
		// otherwise leave the database behind and slowly fill the server.
		if err := control.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name)).Error; err != nil {
			t.Logf("testsupport: drop database %s: %v", name, err)
		}
	})

	database, err := db.OpenDatabase("postgres", "", cfg.dsn(name))
	if err != nil {
		t.Fatalf("testsupport: open %s: %v", name, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("testsupport: migrate %s: %v", name, err)
	}
	return database
}

// uniqueName keeps the test name visible for debugging while staying inside
// PostgreSQL's 63-byte identifier limit.
func uniqueName(testName string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		default:
			return '_'
		}
	}, testName)
	if len(safe) > 20 {
		safe = safe[:20]
	}
	return fmt.Sprintf("%s%s_%d_%d", databasePrefix, safe, time.Now().UnixNano()%1_000_000, rand.Intn(100000))
}
