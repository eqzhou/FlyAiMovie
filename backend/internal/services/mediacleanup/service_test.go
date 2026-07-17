package mediacleanup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestQueueAndProcessIsIdempotent(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/cleanup.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	rel, absolute, err := store.Save("uploads", "private.txt", strings.NewReader("private"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(database, store)
	if err := service.Queue(9, []string{rel, rel}); err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessOrganization(9, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(absolute); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
	result, err = service.ProcessOrganization(9, 100)
	if err != nil || result.Completed != 0 || result.Failed != 0 {
		t.Fatalf("second result=%+v err=%v", result, err)
	}
}

func TestQueueRejectsEscapingPath(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/cleanup.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	service := New(database, storage.NewLocal(t.TempDir()))
	if err := service.Queue(1, []string{"../outside"}); err == nil {
		t.Fatal("expected escaping path error")
	}
}

func TestQueueRequiresConfiguredServiceAndNormalizesPaths(t *testing.T) {
	if err := (*Service)(nil).Queue(1, []string{"a"}); err == nil {
		t.Fatal("nil service accepted")
	}
	database, err := db.Open(t.TempDir() + "/normalize.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	service := New(database, store)
	rel, absolute, err := store.Save("uploads", "one.txt", strings.NewReader("one"))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Queue(2, []string{"/static/" + rel, absolute}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Model(&models.MediaDeletionTask{}).Where("organization_id = ?", 2).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("normalized duplicate count=%d", count)
	}
	if err := service.Queue(2, []string{""}); err == nil {
		t.Fatal("empty cleanup path accepted")
	}
}

func TestProcessRecordsFailureAndRetriesMissingFiles(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/failure.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := storage.NewLocal(t.TempDir())
	directory := filepath.Join(store.Root, "uploads", "non-empty")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "child"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(database, store)
	if err := service.Queue(3, []string{"uploads/non-empty", "uploads/already-missing.txt"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessOrganization(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Completed != 1 {
		t.Fatalf("result=%+v", result)
	}
	var failed models.MediaDeletionTask
	if err := database.Where("organization_id = ? AND status = ?", 3, "failed").First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Attempts != 1 || failed.LastError == "" || failed.AvailableAt <= failed.UpdatedAt {
		t.Fatalf("failed task=%+v", failed)
	}
}
