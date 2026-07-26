package mediamigrate

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return len(entries)
}

// Scan must clamp nonsensical limits to the 1000 default rather than passing them to
// the database as LIMIT 0 / LIMIT -1, which would silently return nothing or everything.
func TestScanClampsOutOfRangeLimits(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	for i := 0; i < 3; i++ {
		asset := models.Asset{OrganizationID: 3, Name: "remote", Type: "image", URL: "https://cdn.example/a" + strconv.Itoa(i) + ".png", CreatedAt: now, UpdatedAt: now}
		if err := service.DB.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, limit := range []int{0, -5, 20000} {
		candidates, err := service.Scan(limit)
		if err != nil {
			t.Fatalf("Scan(%d) error: %v", limit, err)
		}
		if len(candidates) != 3 {
			t.Fatalf("Scan(%d) returned %d candidates, want 3 after clamping to the default", limit, len(candidates))
		}
	}
}

// A valid small limit must actually cap the returned candidate count even though the
// three source tables are queried independently.
func TestScanTruncatesCombinedResultsToLimit(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	asset := models.Asset{OrganizationID: 3, Name: "remote", Type: "image", URL: "https://cdn.example/image.png", CreatedAt: now, UpdatedAt: now}
	image := models.ImageGeneration{OrganizationID: 3, ImageURL: "https://cdn.example/generated.png", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{OrganizationID: 3, VideoURL: "https://cdn.example/generated.mp4", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&asset, &image, &video} {
		if err := service.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := service.Scan(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("Scan(1) returned %d candidates, want the combined list truncated to 1", len(candidates))
	}
}

// A failing query must surface the error instead of reporting an empty candidate list,
// which would make a broken scan look like "nothing to migrate".
func TestScanReturnsDatabaseErrors(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "empty.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: database, Store: storage.NewLocal(t.TempDir())}

	candidates, scanErr := service.Scan(10)
	if scanErr == nil {
		t.Fatalf("Scan() returned candidates=%#v and no error, want the missing-table error", candidates)
	}
	if candidates != nil {
		t.Fatalf("Scan() returned candidates=%#v alongside an error, want nil", candidates)
	}
}

// Scan must skip rows whose media is already local and rows whose URL is not a remote
// http(s) target, per source table.
func TestScanSkipsLocalAndNonRemoteGenerations(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	rows := []any{
		&models.ImageGeneration{OrganizationID: 3, ImageURL: "https://cdn.example/keep.png", CreatedAt: now, UpdatedAt: now},
		&models.ImageGeneration{OrganizationID: 3, ImageURL: "/static/local.png", CreatedAt: now, UpdatedAt: now},
		&models.ImageGeneration{OrganizationID: 3, ImageURL: "https://cdn.example/already.png", LocalPath: "images/already.png", CreatedAt: now, UpdatedAt: now},
		&models.VideoGeneration{OrganizationID: 3, VideoURL: "data:video/mp4;base64,AAAA", CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := service.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates, err := service.Scan(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].SourceURL != "https://cdn.example/keep.png" {
		t.Fatalf("candidates=%+v, want only the remote generation without a local copy", candidates)
	}
}

// A download failure must mark the migration failed with the error recorded, and a
// later successful retry must reuse the same row while incrementing Attempts.
func TestMigrateRetryAfterDownloadFailureIncrementsAttempts(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	asset := models.Asset{OrganizationID: 6, Name: "remote", Type: "image", URL: "https://cdn.example/source.png", CreatedAt: now, UpdatedAt: now}
	if err := service.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	failing := Candidate{OrganizationID: 6, TargetType: "asset", TargetID: asset.ID, SourceURL: "file:///nonexistent/missing.png", Kind: "image"}
	if result := service.Run(context.Background(), []Candidate{failing}); result.Failed != 1 {
		t.Fatalf("result=%+v, want the download failure counted", result)
	}

	var migration models.MediaMigration
	if err := service.DB.Where("organization_id = ? AND target_type = ? AND target_id = ?", 6, "asset", asset.ID).First(&migration).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != "failed" || migration.LastError == "" || migration.Attempts != 1 {
		t.Fatalf("migration=%+v, want a failed first attempt with the error recorded", migration)
	}

	succeeding := Candidate{OrganizationID: 6, TargetType: "asset", TargetID: asset.ID, SourceURL: localMigrationFile(t, "retry.png"), Kind: "image"}
	if result := service.Run(context.Background(), []Candidate{succeeding}); result.Succeeded != 1 {
		t.Fatalf("result=%+v, want the retry to succeed", result)
	}

	var migrations []models.MediaMigration
	if err := service.DB.Where("organization_id = ? AND target_type = ? AND target_id = ?", 6, "asset", asset.ID).Find(&migrations).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 {
		t.Fatalf("migrations=%d, want the retry to reuse the existing row", len(migrations))
	}
	if migrations[0].Status != "completed" || migrations[0].Attempts != 2 {
		t.Fatalf("migration=%+v, want status completed with Attempts incremented to 2", migrations[0])
	}
	if migrations[0].LastError != "" {
		t.Fatalf("migration=%+v, want the previous error cleared on success", migrations[0])
	}
}

// An unknown target type has no update statement, so the transaction must abort and the
// downloaded file must not be left behind.
func TestMigrateRejectsUnknownTargetType(t *testing.T) {
	service := testMigrationService(t)
	candidate := Candidate{OrganizationID: 6, TargetType: "unsupported", TargetID: 1, SourceURL: localMigrationFile(t, "unknown-type.png"), Kind: "image"}

	result := service.Run(context.Background(), []Candidate{candidate})
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result=%+v, want the unknown target type to fail", result)
	}
	var migration models.MediaMigration
	if err := service.DB.Where("organization_id = ? AND target_type = ?", 6, "unsupported").First(&migration).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != "failed" || migration.LastError == "" {
		t.Fatalf("migration=%+v, want a failed record with the error recorded", migration)
	}
	if got := countFiles(t, filepath.Join(service.Store.Root, "images")); got != 0 {
		t.Fatalf("images dir holds %d files, want the download cleaned up after rollback", got)
	}
}

// image_generation and video_generation targets update their own URL columns.
func TestMigrateUpdatesGenerationTargets(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	image := models.ImageGeneration{OrganizationID: 6, ImageURL: "https://cdn.example/gen.png", CreatedAt: now, UpdatedAt: now}
	video := models.VideoGeneration{OrganizationID: 6, VideoURL: "https://cdn.example/gen.mp4", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&image, &video} {
		if err := service.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates := []Candidate{
		{OrganizationID: 6, TargetType: "image_generation", TargetID: image.ID, SourceURL: localMigrationFile(t, "gen-image.png"), Kind: "image"},
		{OrganizationID: 6, TargetType: "video_generation", TargetID: video.ID, SourceURL: localMigrationFile(t, "gen-video.mp4"), Kind: "video"},
	}
	if result := service.Run(context.Background(), candidates); result.Succeeded != 2 {
		t.Fatalf("result=%+v, want both generation targets migrated", result)
	}

	if err := service.DB.First(&image, image.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DB.First(&video, video.ID).Error; err != nil {
		t.Fatal(err)
	}
	if image.LocalPath == "" || image.ImageURL == "https://cdn.example/gen.png" {
		t.Fatalf("image=%+v, want image_url and local_path rewritten", image)
	}
	if video.LocalPath == "" || video.VideoURL == "https://cdn.example/gen.mp4" {
		t.Fatalf("video=%+v, want video_url and local_path rewritten", video)
	}
	if _, err := os.Stat(service.Store.Abs(image.LocalPath)); err != nil {
		t.Fatalf("localized image missing: %v", err)
	}
	if _, err := os.Stat(service.Store.Abs(video.LocalPath)); err != nil {
		t.Fatalf("localized video missing: %v", err)
	}
}

// Two targets sharing identical bytes must collapse onto one cached object, and the
// duplicate download must be deleted rather than orphaned on disk.
func TestMigrateDeduplicatesIdenticalContentAndRemovesDuplicateFile(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	first := models.Asset{OrganizationID: 6, Name: "first", Type: "image", URL: "https://cdn.example/one.png", CreatedAt: now, UpdatedAt: now}
	second := models.Asset{OrganizationID: 6, Name: "second", Type: "image", URL: "https://cdn.example/two.png", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&first, &second} {
		if err := service.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	candidates := []Candidate{
		{OrganizationID: 6, TargetType: "asset", TargetID: first.ID, SourceURL: localMigrationFile(t, "dupe-a.png"), Kind: "image"},
		{OrganizationID: 6, TargetType: "asset", TargetID: second.ID, SourceURL: localMigrationFile(t, "dupe-b.png"), Kind: "image"},
	}
	if result := service.Run(context.Background(), candidates); result.Succeeded != 2 {
		t.Fatalf("result=%+v, want both assets migrated", result)
	}

	if err := service.DB.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DB.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.LocalPath != second.LocalPath {
		t.Fatalf("first=%q second=%q, want identical content to share one local path", first.LocalPath, second.LocalPath)
	}
	if got := countFiles(t, filepath.Join(service.Store.Root, "images")); got != 1 {
		t.Fatalf("images dir holds %d files, want the duplicate download removed", got)
	}
	if _, err := os.Stat(service.Store.Abs(first.LocalPath)); err != nil {
		t.Fatalf("canonical file missing after dedupe: %v", err)
	}

	var objects int64
	if err := service.DB.Model(&models.MediaCacheObject{}).Where("organization_id = ?", 6).Count(&objects).Error; err != nil {
		t.Fatal(err)
	}
	if objects != 1 {
		t.Fatalf("cache objects=%d, want identical content stored once", objects)
	}
}

// If rewriting downstream references fails, the whole migration must roll back: the
// target row keeps its original URL and the downloaded file is removed.
func TestMigrateRollsBackWhenReferenceRewriteFails(t *testing.T) {
	service := testMigrationService(t)
	now := response.Now()
	sourceURL := "https://cdn.example/rollback.png"
	asset := models.Asset{OrganizationID: 6, Name: "remote", Type: "image", URL: sourceURL, CreatedAt: now, UpdatedAt: now}
	if err := service.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	// grid_histories is rewritten for image migrations; dropping it makes that step fail.
	if err := service.DB.Migrator().DropTable(&models.GridHistory{}); err != nil {
		t.Fatal(err)
	}

	candidate := Candidate{OrganizationID: 6, TargetType: "asset", TargetID: asset.ID, SourceURL: localMigrationFile(t, "rollback.png"), Kind: "image"}
	result := service.Run(context.Background(), []Candidate{candidate})
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result=%+v, want the reference rewrite failure to fail the migration", result)
	}

	if err := service.DB.First(&asset, asset.ID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.URL != sourceURL || asset.LocalPath != "" {
		t.Fatalf("asset=%+v, want the target row rolled back to its remote URL", asset)
	}
	var migration models.MediaMigration
	if err := service.DB.Where("organization_id = ? AND target_id = ?", 6, asset.ID).First(&migration).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != "failed" || migration.LastError == "" {
		t.Fatalf("migration=%+v, want a failed record with the error recorded", migration)
	}
	if got := countFiles(t, filepath.Join(service.Store.Root, "images")); got != 0 {
		t.Fatalf("images dir holds %d files, want the download cleaned up after rollback", got)
	}
}

// Audio migrations rewrite the audio reference set (character voice samples, storyboard
// TTS audio) and must leave image-only columns untouched.
func TestReplaceMediaReferencesHandlesAudioAndVideoKinds(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/kinds.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	oldAudio := "https://cdn.example/old.mp3"
	oldVideo := "https://cdn.example/old.mp4"
	character := models.Character{OrganizationID: 7, DramaID: 1, Name: "voiced", VoiceSampleURL: oldAudio, ImageURL: oldAudio, CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, TTSAudioURL: oldAudio, VideoURL: oldVideo, CreatedAt: now, UpdatedAt: now}
	merge := models.VideoMerge{OrganizationID: 7, Title: "merge", MergedURL: oldVideo, CreatedAt: now}
	for _, row := range []any{&character, &storyboard, &merge} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := replaceMediaReferences(database, Candidate{OrganizationID: 7, Kind: "audio", SourceURL: oldAudio}, "/static/audio/new.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := replaceMediaReferences(database, Candidate{OrganizationID: 7, Kind: "video", SourceURL: oldVideo}, "/static/videos/new.mp4"); err != nil {
		t.Fatal(err)
	}

	database.First(&character, character.ID)
	database.First(&storyboard, storyboard.ID)
	database.First(&merge, merge.ID)
	if character.VoiceSampleURL != "/static/audio/new.mp3" || storyboard.TTSAudioURL != "/static/audio/new.mp3" {
		t.Fatalf("audio references not rewritten: character=%+v storyboard=%+v", character, storyboard)
	}
	if character.ImageURL != oldAudio {
		t.Fatalf("character=%+v, want image_url untouched by an audio migration", character)
	}
	if storyboard.VideoURL != "/static/videos/new.mp4" || merge.MergedURL != "/static/videos/new.mp4" {
		t.Fatalf("video references not rewritten: storyboard=%+v merge=%+v", storyboard, merge)
	}
}
