package servicebundle

import (
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func bundleDrafts() []DraftItem {
	active := true
	return []DraftItem{
		{ServiceType: "video", Provider: "openai", Name: "Video", BaseURL: "https://api.example.test", Model: "video", IsDefault: true, IsActive: &active},
		{ServiceType: "text", Provider: "openai", Name: "Text", BaseURL: "https://api.example.test", Model: "text", IsDefault: true, IsActive: &active},
		{ServiceType: "audio", Provider: "minimax", Name: "Audio", BaseURL: "https://api.example.test", Model: "audio", IsDefault: true, IsActive: &active},
		{ServiceType: "image", Provider: "openai", Name: "Image", BaseURL: "https://api.example.test", Model: "image", IsDefault: true, IsActive: &active},
	}
}

func TestNormalizeRequiresAndOrdersAllFourServiceTypes(t *testing.T) {
	normalized, err := Normalize(bundleDrafts())
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"text", "image", "video", "audio"} {
		if normalized[index].ServiceType != want {
			t.Fatalf("normalized[%d]=%q want %q", index, normalized[index].ServiceType, want)
		}
	}
	if _, err := Normalize(bundleDrafts()[:3]); err == nil {
		t.Fatal("expected missing service validation error")
	}
	duplicates := bundleDrafts()
	duplicates[3].ServiceType = "text"
	if _, err := Normalize(duplicates); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("duplicate validation err=%v", err)
	}
}

func TestBuiltinLookupAndTokenAreStableWithoutMutatingInputs(t *testing.T) {
	template, ok := FindBuiltin("standard-cloud-studio")
	if !ok || len(template.Services) != 4 {
		t.Fatalf("builtin=%+v found=%v", template, ok)
	}
	if _, ok := FindBuiltin("missing"); ok {
		t.Fatal("unexpected missing builtin")
	}
	items := bundleDrafts()
	rows := []models.AIServiceConfig{{ID: 2, OrganizationID: 7, ServiceType: "text"}, {ID: 1, OrganizationID: 7, ServiceType: "image"}}
	first, err := Token(7, items, rows)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Token(7, []DraftItem{items[3], items[2], items[1], items[0]}, []models.AIServiceConfig{rows[1], rows[0]})
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("stable token first=%q second=%q err=%v", first, second, err)
	}
	if rows[0].ID != 2 || items[0].ServiceType != "video" {
		t.Fatal("Token mutated caller-owned slices")
	}
}

func TestPlanReportsReuseAndDefaultReplacement(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.AIServiceConfig{}, &models.AgentConfig{}); err != nil {
		t.Fatal(err)
	}
	rows := []models.AIServiceConfig{
		{OrganizationID: 7, ServiceType: "text", Provider: "openai", Name: "Text", BaseURL: "https://api.example.test", Model: "text", IsDefault: false},
		{OrganizationID: 7, ServiceType: "text", Provider: "openai", Name: "Old default", BaseURL: "https://old.example.test", Model: "old", IsDefault: true},
		{OrganizationID: 8, ServiceType: "text", Provider: "openai", Name: "Text", BaseURL: "https://api.example.test", Model: "text", IsDefault: true},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := Plan(database, 7, bundleDrafts(), true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Items[0].Action != "reuse" || preview.Items[0].ConfigID != rows[0].ID || len(preview.PreviewToken) != 64 {
		t.Fatalf("preview=%+v", preview)
	}
	var reused, replaced bool
	for _, conflict := range preview.Conflicts {
		reused = reused || conflict.Kind == "reused"
		replaced = replaced || conflict.Kind == "default_replaced"
	}
	if !reused || !replaced {
		t.Fatalf("conflicts=%+v", preview.Conflicts)
	}
}

func TestPlanIncludesFiveAgentDefaultsAndTracksTheirState(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.AIServiceConfig{}, &models.AgentConfig{}); err != nil {
		t.Fatal(err)
	}
	deleted := "2026-08-01T00:00:00Z"
	existing := []models.AgentConfig{
		{OrganizationID: 7, AgentType: "script_rewriter", Name: "Custom writer", Model: "text", SystemPrompt: "keep me", IsActive: true},
		{OrganizationID: 7, AgentType: "extractor", Name: "Custom extractor", Model: "old-model", IsActive: false, DeletedAt: &deleted},
	}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	preview, err := Plan(database, 7, bundleDrafts(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Agents) != 5 {
		t.Fatalf("agents=%+v", preview.Agents)
	}
	wantActions := map[string]string{
		"script_rewriter":       "reuse",
		"extractor":             "update",
		"storyboard_breaker":    "create",
		"voice_assigner":        "create",
		"grid_prompt_generator": "create",
	}
	for _, planned := range preview.Agents {
		if planned.Action != wantActions[planned.AgentType] || planned.Model != "text" {
			t.Fatalf("planned agent=%+v", planned)
		}
	}

	before := preview.PreviewToken
	if err := database.Model(&models.AgentConfig{}).Where("id = ?", existing[0].ID).Update("model", "changed-after-preview").Error; err != nil {
		t.Fatal(err)
	}
	after, err := Plan(database, 7, bundleDrafts(), true)
	if err != nil {
		t.Fatal(err)
	}
	if before == after.PreviewToken {
		t.Fatal("agent config change did not invalidate preview token")
	}

	withoutAgents, err := Plan(database, 7, bundleDrafts(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutAgents.Agents) != 0 {
		t.Fatalf("agents should be omitted when disabled: %+v", withoutAgents.Agents)
	}
}
