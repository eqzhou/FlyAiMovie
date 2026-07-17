package mediacleanup

import (
	"os"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
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
