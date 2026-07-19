package db

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/gorm"
)

func TestDatabaseLoggerSuppressesExpectedMisses(t *testing.T) {
	var output bytes.Buffer
	databaseLogger := newDatabaseLogger(&output)
	databaseLogger.Trace(context.Background(), time.Now(), func() (string, int64) {
		return "SELECT * FROM users WHERE email = 'private@example.com'", 0
	}, gorm.ErrRecordNotFound)
	if output.Len() != 0 {
		t.Fatalf("record-not-found query was logged: %s", output.String())
	}

	databaseLogger.Trace(context.Background(), time.Now(), func() (string, int64) {
		return "SELECT 1", 0
	}, errors.New("database unavailable"))
	if !strings.Contains(output.String(), "database unavailable") {
		t.Fatalf("real database error was not logged: %s", output.String())
	}
}

func TestOpenDatabaseValidation(t *testing.T) {
	if _, err := OpenDatabase("postgres", "", ""); err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("expected missing postgres DSN error, got %v", err)
	}
	if _, err := OpenDatabase("unknown", "", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported database error, got %v", err)
	}
}

func TestSeedDefaultsIncludesOpenAIVideoAndIsRepeatable(t *testing.T) {
	database, err := Open(t.TempDir() + "/seed.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := SeedDefaults(database); err != nil {
		t.Fatal(err)
	}
	if err := SeedDefaults(database); err != nil {
		t.Fatalf("repeat seed: %v", err)
	}

	var provider models.AIServiceProvider
	if err := database.Where("name = ? AND service_type = ? AND provider = ?", "openai-video", "video", "openai").First(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if provider.DefaultURL != "https://api.openai.com" || !strings.Contains(provider.PresetModels, "sora-2-pro") || !provider.IsActive {
		t.Fatalf("unexpected provider: %+v", provider)
	}
	var mockCount int64
	if err := database.Model(&models.AIServiceConfig{}).Where("provider = ?", "mock").Count(&mockCount).Error; err != nil {
		t.Fatal(err)
	}
	if mockCount != 4 {
		t.Fatalf("mock configs=%d want 4", mockCount)
	}
}

func TestSeedOrganizationDefaultsIsIdempotentAndIsolated(t *testing.T) {
	database, err := Open(t.TempDir() + "/organization-seed.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	for _, organizationID := range []uint{11, 22, 11} {
		if err := SeedOrganizationDefaults(database, organizationID); err != nil {
			t.Fatalf("seed organization %d: %v", organizationID, err)
		}
	}
	for _, organizationID := range []uint{11, 22} {
		var mockCount, agentCount, promptCount, revisionCount int64
		if err := database.Model(&models.AIServiceConfig{}).Where("organization_id = ? AND provider = ?", organizationID, "mock").Count(&mockCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&models.AgentConfig{}).Where("organization_id = ? AND deleted_at IS NULL", organizationID).Count(&agentCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&models.PromptTemplate{}).Where("organization_id = ? AND deleted_at IS NULL", organizationID).Count(&promptCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&models.PromptTemplateRevision{}).Where("organization_id = ?", organizationID).Count(&revisionCount).Error; err != nil {
			t.Fatal(err)
		}
		if mockCount != 4 || agentCount != 5 || promptCount != 8 || revisionCount != 8 {
			t.Fatalf("organization %d defaults: mock=%d agents=%d prompts=%d revisions=%d", organizationID, mockCount, agentCount, promptCount, revisionCount)
		}
	}
}

func TestAutoMigrateBackfillsCurrentPromptTemplateRevision(t *testing.T) {
	database, err := Open(t.TempDir() + "/prompt-revision-backfill.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	template := models.PromptTemplate{OrganizationID: 17, Key: "legacy_prompt", Name: "Legacy", Category: "image", Content: "legacy content", VariablesJSON: "[]", Version: 4, IsActive: true, CreatedAt: "old", UpdatedAt: "new"}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	var revision models.PromptTemplateRevision
	if err := database.Where("organization_id = ? AND prompt_template_id = ? AND version = ?", 17, template.ID, 4).First(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if revision.Content != "legacy content" || revision.CreatedAt != "new" {
		t.Fatalf("unexpected backfilled revision: %+v", revision)
	}
}

func TestAutoMigrateDropsObsoleteGridCellURLIndex(t *testing.T) {
	database, err := Open(t.TempDir() + "/obsolete-grid-index.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE UNIQUE INDEX idx_asset_grid_cell ON assets (organization_id, grid_history_id, url)").Error; err != nil {
		t.Fatal(err)
	}
	if !database.Migrator().HasIndex(&models.Asset{}, "idx_asset_grid_cell") {
		t.Fatal("test setup did not create obsolete grid index")
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if database.Migrator().HasIndex(&models.Asset{}, "idx_asset_grid_cell") {
		t.Fatal("obsolete grid cell URL index still exists after migration")
	}
}

func TestSeedOrganizationDefaultsPreservesUnsupportedLegacyPrompt(t *testing.T) {
	database, err := Open(t.TempDir() + "/legacy-prompt.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	legacy := models.AgentConfig{OrganizationID: 33, AgentType: "script_rewriter", Name: "Legacy", SystemPrompt: "keep {{unsupported}}", IsActive: true, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := SeedOrganizationDefaults(database, 33); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Model(&models.PromptTemplate{}).Where("organization_id = ? AND key = ?", 33, "script_rewriter").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unsupported legacy prompt was migrated, count=%d", count)
	}
}

func TestMigrationAndSeedingReportClosedDatabase(t *testing.T) {
	database, err := Open(t.TempDir() + "/closed.db")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err == nil {
		t.Fatal("migration on closed database succeeded")
	}
	if err := SeedDefaults(database); err == nil {
		t.Fatal("seeding on closed database succeeded")
	}
}
