package mediacache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func testService(t *testing.T) *Service {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/cache.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return New(database, storage.NewLocal(t.TempDir()))
}

func TestPutDeduplicatesWithinOrganizationAndIsolatesOrganizations(t *testing.T) {
	service := testService(t)
	firstPath, _, err := service.Store.SaveBytes("images", "a.png", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	duplicatePath, _, err := service.Store.SaveBytes("images", "duplicate.png", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	first := PutInput{OrganizationID: 1, Namespace: "asset", Key: "1", ContentHash: "abc", Kind: "image", LocalPath: firstPath, PublicURL: service.Store.PublicURL(firstPath), Size: 12}
	objectA, reused, err := service.Put(first)
	if err != nil || reused {
		t.Fatalf("first object=%+v reused=%v err=%v", objectA, reused, err)
	}
	second := first
	second.Key = "2"
	second.LocalPath = duplicatePath
	second.PublicURL = service.Store.PublicURL(duplicatePath)
	objectB, reused, err := service.Put(second)
	if err != nil || !reused || objectB.ID != objectA.ID || objectB.ReferenceCount != 2 {
		t.Fatalf("second object=%+v reused=%v err=%v", objectB, reused, err)
	}
	other := first
	other.OrganizationID = 2
	other.Key = "1"
	objectOther, reused, err := service.Put(other)
	if err != nil || reused || objectOther.ID == objectA.ID {
		t.Fatalf("other object=%+v reused=%v err=%v", objectOther, reused, err)
	}
	resolved, err := service.Resolve(1, "asset", "2")
	if err != nil || resolved.LocalPath != firstPath {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
}

func TestPutReplacesReferenceAndReleaseOrphansOldObject(t *testing.T) {
	service := testService(t)
	oldObject, _, err := service.Put(PutInput{OrganizationID: 3, Namespace: "job", Key: "9", ContentHash: "old", Kind: "result", LocalPath: "results/old.json"})
	if err != nil {
		t.Fatal(err)
	}
	newObject, reused, err := service.Put(PutInput{OrganizationID: 3, Namespace: "job", Key: "9", ContentHash: "new", Kind: "result", LocalPath: "results/new.json"})
	if err != nil || reused || newObject.ID == oldObject.ID {
		t.Fatalf("new=%+v reused=%v err=%v", newObject, reused, err)
	}
	var storedOld models.MediaCacheObject
	if err := service.DB.First(&storedOld, oldObject.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOld.ReferenceCount != 0 || storedOld.Status != StatusOrphaned || storedOld.ExpiresAt == nil {
		t.Fatalf("old object not orphaned: %+v", storedOld)
	}
	if err := service.Release(3, "job", "9"); err != nil {
		t.Fatal(err)
	}
	if err := service.Release(3, "job", "9"); err != nil {
		t.Fatalf("release must be idempotent: %v", err)
	}
	var storedNew models.MediaCacheObject
	if err := service.DB.First(&storedNew, newObject.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedNew.ReferenceCount != 0 || storedNew.Status != StatusOrphaned {
		t.Fatalf("new object not orphaned: %+v", storedNew)
	}
}

func TestPurgeExpiredQueuesDeletionAndPreservesLiveReferences(t *testing.T) {
	service := testService(t)
	oldRel, oldAbs, err := service.Store.SaveBytes("cache", "old.bin", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	liveRel, _, err := service.Store.SaveBytes("cache", "live.bin", []byte("live"))
	if err != nil {
		t.Fatal(err)
	}
	oldObject, _, err := service.Put(PutInput{OrganizationID: 5, Namespace: "external", Key: "old", ContentHash: "old-hash", Kind: "media", LocalPath: oldRel})
	if err != nil {
		t.Fatal(err)
	}
	liveObject, _, err := service.Put(PutInput{OrganizationID: 5, Namespace: "external", Key: "live", ContentHash: "live-hash", Kind: "media", LocalPath: liveRel})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Release(5, "external", "old"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := service.DB.Model(&models.MediaCacheObject{}).Where("id = ?", oldObject.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	result, err := service.PurgeExpired(5, 100)
	if err != nil || result.Queued != 1 || result.DeletedObjects != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(oldAbs); err != nil {
		t.Fatalf("cleanup should be compensating, file removed too early: %v", err)
	}
	var task models.MediaDeletionTask
	if err := service.DB.Where("organization_id = ? AND local_path = ?", 5, oldRel).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Reason != "cache_expired" || task.Status != "pending" {
		t.Fatalf("task=%+v", task)
	}
	if err := service.DB.First(&models.MediaCacheObject{}, liveObject.ID).Error; err != nil {
		t.Fatalf("live object was purged: %v", err)
	}
	second, err := service.PurgeExpired(5, 100)
	if err != nil || second.Queued != 0 || second.DeletedObjects != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestExpireReferencesReleasesObject(t *testing.T) {
	service := testService(t)
	expires := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	object, _, err := service.Put(PutInput{OrganizationID: 8, Namespace: "ai", Key: "request", ContentHash: "result", Kind: "json", LocalPath: "ai/result.json", ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PurgeExpired(8, 100)
	if err != nil || result.ReleasedReferences != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var stored models.MediaCacheObject
	if err := service.DB.First(&stored, object.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ReferenceCount != 0 || stored.Status != StatusOrphaned {
		t.Fatalf("stored=%+v", stored)
	}
	if stored.LastAccessedAt == "" || stored.UpdatedAt == "" || stored.CreatedAt == "" || stored.CreatedAt > response.Now() {
		t.Fatalf("timestamps=%+v", stored)
	}
}

func TestInlineValuesAreCachedAndExpire(t *testing.T) {
	service := testService(t)
	object, reused, err := service.PutValue(12, "ai_request", "prompt-hash", "text", `{"answer":"ok"}`, time.Hour)
	if err != nil || reused || object.Payload == "" {
		t.Fatalf("object=%+v reused=%v err=%v", object, reused, err)
	}
	value, err := service.ResolveValue(12, "ai_request", "prompt-hash")
	if err != nil || value != `{"answer":"ok"}` {
		t.Fatalf("value=%q err=%v", value, err)
	}
	var reference models.MediaCacheReference
	if err := service.DB.Where("organization_id = ? AND namespace = ?", 12, "ai_request").First(&reference).Error; err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := service.DB.Model(&reference).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveValue(12, "ai_request", "prompt-hash"); err == nil {
		t.Fatal("expired value resolved")
	}
}

func TestStatsHashAndPurgeAllOrganizations(t *testing.T) {
	service := testService(t)
	for organizationID, content := range map[uint]string{20: "first", 21: "second"} {
		relative, absolute, err := service.Store.SaveBytes("cache", content+".bin", []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		hash, size, err := HashFile(absolute)
		if err != nil {
			t.Fatal(err)
		}
		expected := sha256.Sum256([]byte(content))
		if hash != hex.EncodeToString(expected[:]) || size != int64(len(content)) {
			t.Fatalf("hash=%q size=%d", hash, size)
		}
		object, _, err := service.Put(PutInput{OrganizationID: organizationID, Namespace: "asset", Key: "1", ContentHash: hash, Kind: "binary", LocalPath: relative, Size: size})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Release(organizationID, "asset", "1"); err != nil {
			t.Fatal(err)
		}
		expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
		if err := service.DB.Model(&models.MediaCacheObject{}).Where("id = ?", object.ID).Update("expires_at", expired).Error; err != nil {
			t.Fatal(err)
		}
	}
	stats, err := service.Stats(20)
	if err != nil || stats.Objects != 1 || stats.References != 0 || stats.Bytes != 5 || stats.Orphaned != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	result, err := service.PurgeAllExpired(10)
	if err != nil || result.DeletedObjects != 2 || result.Queued != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, _, err := HashFile(service.Store.Abs("missing.bin")); err == nil {
		t.Fatal("missing file hashed")
	}
}

func TestPutIsIdempotentForSameReferenceAndReplacesMissingCanonicalFile(t *testing.T) {
	service := testService(t)
	firstPath, firstAbsolute, err := service.Store.SaveBytes("cache", "first.bin", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	input := PutInput{OrganizationID: 30, Namespace: "asset", Key: "one", ContentHash: "same-hash", Kind: "binary", LocalPath: firstPath}
	object, _, err := service.Put(input)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	input.ExpiresAt = &expires
	again, reused, err := service.Put(input)
	if err != nil || !reused || again.ID != object.ID || again.ReferenceCount != 1 {
		t.Fatalf("again=%+v reused=%v err=%v", again, reused, err)
	}
	if err := os.Remove(firstAbsolute); err != nil {
		t.Fatal(err)
	}
	secondPath, _, err := service.Store.SaveBytes("cache", "second.bin", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	input.Key, input.LocalPath, input.ExpiresAt = "two", secondPath, nil
	recovered, reused, err := service.Put(input)
	if err != nil || reused || recovered.LocalPath != secondPath || recovered.ReferenceCount != 2 {
		t.Fatalf("recovered=%+v reused=%v err=%v", recovered, reused, err)
	}
}

func TestCacheInputAndPurgeErrorsAreExplicit(t *testing.T) {
	service := testService(t)
	invalid := []PutInput{
		{},
		{Namespace: strings.Repeat("n", 65), Key: "key", ContentHash: "hash", Kind: "kind"},
		{Namespace: "ns", Key: "key", ContentHash: "hash", Kind: "kind", LocalPath: "../escape"},
		{Namespace: "ns", Key: "key", ContentHash: "hash", Kind: "kind", Size: -1},
	}
	for _, input := range invalid {
		if _, _, err := service.Put(input); err == nil {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
	if _, err := service.Resolve(0, "", ""); err == nil {
		t.Fatal("empty cache reference resolved")
	}
	if _, _, err := service.PutValue(1, "value", "large", "json", strings.Repeat("x", (10<<20)+1), time.Hour); err == nil {
		t.Fatal("oversized inline value accepted")
	}
	object, _, err := service.Put(PutInput{OrganizationID: 40, Namespace: "asset", Key: "1", ContentHash: "hash", Kind: "binary", LocalPath: "cache/file.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Release(40, "asset", "1"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := service.DB.Model(&models.MediaCacheObject{}).Where("id = ?", object.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	withoutStorage := New(service.DB, nil)
	if _, err := withoutStorage.PurgeExpired(40, 0); err == nil || !strings.Contains(err.Error(), "storage") {
		t.Fatalf("purge error=%v", err)
	}
}
