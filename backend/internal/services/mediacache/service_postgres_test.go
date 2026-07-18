package mediacache

import (
	"crypto/rand"
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
)

func TestPostgresCacheLifecycle(t *testing.T) {
	configPath := os.Getenv("FLYAIMOVIE_POSTGRES_TEST_CONFIG")
	if configPath == "" {
		t.Skip("set FLYAIMOVIE_POSTGRES_TEST_CONFIG to run PostgreSQL integration")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Type != "postgres" && cfg.Database.Type != "postgresql" {
		t.Skip("configured database is not PostgreSQL")
	}
	database, err := db.OpenDatabase(cfg.Database.Type, cfg.Database.Path, cfg.Database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	var randomBytes [4]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		t.Fatal(err)
	}
	organizationID := uint(binary.BigEndian.Uint32(randomBytes[:])) + 1_000_000_000
	t.Cleanup(func() {
		database.Where("organization_id = ?", organizationID).Delete(&models.MediaCacheReference{})
		database.Where("organization_id = ?", organizationID).Delete(&models.MediaCacheObject{})
		database.Where("organization_id = ?", organizationID).Delete(&models.MediaDeletionTask{})
	})
	service := New(database, nil)
	object, _, err := service.PutValue(organizationID, "postgres_test", "result", "json", `{"ok":true}`, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := service.ResolveValue(organizationID, "postgres_test", "result"); err != nil || value != `{"ok":true}` {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if err := service.Release(organizationID, "postgres_test", "result"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := database.Model(&models.MediaCacheObject{}).Where("id = ?", object.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.PurgeExpired(organizationID, 10)
	if err != nil || result.DeletedObjects != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
