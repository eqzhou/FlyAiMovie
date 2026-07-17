package mediamigrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/storage"
)

func TestIsRemote(t *testing.T) {
	if !IsRemote("https://cdn.example/a.png") {
		t.Fatal("expected remote URL")
	}
	for _, value := range []string{"/static/a.png", "file:///tmp/a.png", "", "not a url"} {
		if IsRemote(value) {
			t.Fatalf("unexpected remote %q", value)
		}
	}
}

func TestReplaceMediaReferencesIsOrganizationScoped(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/migration.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	oldURL := "https://cdn.example/old.png"
	owned := models.Character{OrganizationID: 7, DramaID: 1, Name: "owned", ImageURL: oldURL, CreatedAt: now, UpdatedAt: now}
	other := models.Character{OrganizationID: 8, DramaID: 2, Name: "other", ImageURL: oldURL, CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, FirstFrameImage: oldURL, LastFrameImage: oldURL, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := replaceMediaReferences(database, Candidate{OrganizationID: 7, Kind: "image", SourceURL: oldURL}, "/static/images/local.png"); err != nil {
		t.Fatal(err)
	}
	database.First(&owned, owned.ID)
	database.First(&other, other.ID)
	database.First(&storyboard, storyboard.ID)
	if owned.ImageURL != "/static/images/local.png" || storyboard.FirstFrameImage != "/static/images/local.png" || storyboard.LastFrameImage != "/static/images/local.png" {
		t.Fatalf("owned references not replaced: owned=%+v storyboard=%+v", owned, storyboard)
	}
	if other.ImageURL != oldURL {
		t.Fatalf("cross-organization reference changed: %+v", other)
	}
}

func TestScanFindsOnlyExternalMediaWithoutLocalCopies(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	asset := models.Asset{OrganizationID: 3, Name: "remote image", Type: "image", URL: "https://cdn.example/image.png", CreatedAt: now, UpdatedAt: now}
	localAsset := models.Asset{OrganizationID: 3, Name: "local", Type: "image", URL: "/static/image.png", LocalPath: "image.png", CreatedAt: now, UpdatedAt: now}
	image := models.ImageGeneration{OrganizationID: 3, ImageURL: "https://cdn.example/generated.png", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{OrganizationID: 3, VideoURL: "https://cdn.example/generated.mp4", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&asset, &localAsset, &image, &video} {
		if err := service.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := service.Scan(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates=%+v", candidates)
	}
	if candidates[0].TargetType != "asset" || candidates[1].TargetType != "image_generation" || candidates[2].TargetType != "video_generation" {
		t.Fatalf("candidate order/types=%+v", candidates)
	}
}

func TestRunMigratesMediaAndIsRepeatable(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	asset := models.Asset{OrganizationID: 4, Name: "remote", Type: "image", URL: "https://cdn.example/source.png", CreatedAt: now, UpdatedAt: now}
	character := models.Character{OrganizationID: 4, DramaID: 1, Name: "character", ImageURL: asset.URL, CreatedAt: now, UpdatedAt: now}
	if err := service.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	sourceURL := localMigrationFile(t, "successful.png")
	candidate := Candidate{OrganizationID: 4, TargetType: "asset", TargetID: asset.ID, SourceURL: sourceURL, Kind: "image"}
	first := service.Run(context.Background(), []Candidate{candidate})
	second := service.Run(context.Background(), []Candidate{candidate})
	if first.Succeeded != 1 || second.Succeeded != 1 || first.Failed != 0 || second.Failed != 0 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if err := service.DB.First(&asset, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.LocalPath == "" || !strings.HasPrefix(asset.URL, "/static/") {
		t.Fatalf("asset=%+v", asset)
	}
	if _, err := os.Stat(service.Store.Abs(asset.LocalPath)); err != nil {
		t.Fatalf("localized media missing: %v", err)
	}
	var migrations int64
	if err := service.DB.Model(&models.MediaMigration{}).Where("organization_id = ? AND target_type = ? AND target_id = ?", 4, "asset", asset.ID).Count(&migrations).Error; err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("migration records=%d", migrations)
	}
}

func TestRunFailsWhenTargetDisappearsAndRemovesDownloadedFile(t *testing.T) {
	service := testMigrationService(t)
	candidate := Candidate{OrganizationID: 5, TargetType: "asset", TargetID: 999, SourceURL: localMigrationFile(t, "missing-target.png"), Kind: "image"}
	result := service.Run(context.Background(), []Candidate{candidate})
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result=%+v", result)
	}
	var migration models.MediaMigration
	if err := service.DB.Where("organization_id = ? AND target_type = ? AND target_id = ?", 5, "asset", 999).First(&migration).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != "failed" || migration.LastError == "" {
		t.Fatalf("migration=%+v", migration)
	}
	files, err := os.ReadDir(filepath.Join(service.Store.Root, "images"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("orphaned files=%v", files)
	}
}

func TestMigrationKindHelpers(t *testing.T) {
	for _, tc := range []struct{ input, kind, subdir string }{
		{"image", "image", "images"}, {"AUDIO", "audio", "audio"}, {"video", "video", "videos"}, {"other", "video", "videos"},
	} {
		if got := mediaKind(tc.input); got != tc.kind {
			t.Errorf("mediaKind(%q)=%q", tc.input, got)
		}
		if got := migrationSubdir(tc.kind); got != tc.subdir {
			t.Errorf("migrationSubdir(%q)=%q", tc.kind, got)
		}
	}
}

func testMigrationService(t *testing.T) *Service {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/migration-service.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return &Service{DB: database, Store: storage.NewLocal(t.TempDir())}
}

func localMigrationFile(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("migration fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return "file://" + path
}
