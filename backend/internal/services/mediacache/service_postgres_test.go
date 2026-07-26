package mediacache

import (
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/testsupport"
)

// The whole suite now runs on PostgreSQL, so this no longer needs an opt-in
// environment variable. It stays separate from the unit tests because it walks
// one cache object through its entire lifecycle -- store, resolve, release,
// expire, purge -- which is where engine-specific behaviour would show up.
func TestPostgresCacheLifecycle(t *testing.T) {
	database := testsupport.OpenDatabase(t)
	const organizationID uint = 1_000_000_001
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
