package prompttemplate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openPromptTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "prompt.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.PromptTemplate{}, &models.Drama{}, &models.Episode{}, &models.Storyboard{}, &models.Character{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestApplyTemplateUsesOrganizationContent(t *testing.T) {
	database := openPromptTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 7, Key: "storyboard_image", Name: "Custom Image", Category: "image",
		Content: "{{drama_title}} / {{shot_title}} / {{shot_description}}", VariablesJSON: `["drama_title","shot_title","shot_description"]`,
		Version: 3, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{Title: "归途"}
	episode := models.Episode{Title: "重逢"}
	storyboard := models.Storyboard{Title: "站台", Description: "两人相遇"}
	resolution := FramePrompt(database, 7, drama, episode, storyboard, "first_frame", "", nil, nil)
	if resolution.Source != "organization_template" || resolution.Version != 3 {
		t.Fatalf("resolution=%+v", resolution)
	}
	if !strings.Contains(resolution.Prompt, "opening frame, 归途 / 站台 / 两人相遇") {
		t.Fatalf("prompt=%q", resolution.Prompt)
	}
}

func TestGridPromptFallsBackWhenTemplateMissing(t *testing.T) {
	database := openPromptTestDB(t)
	shots := []models.Storyboard{{Title: "开场", ImagePrompt: "rain station"}}
	resolution, cells := GridPrompt(database, 9, models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}, "first_frame", 1, 1, shots, []string{"阿宁"}, []string{"站台"})
	if resolution.Source != "fallback" {
		t.Fatalf("resolution=%+v", resolution)
	}
	if len(cells) != 1 || cells[0] != "rain station" {
		t.Fatalf("cells=%#v", cells)
	}
	if !strings.Contains(resolution.Prompt, "1x1") || !strings.Contains(resolution.Prompt, "Panel 1: rain station") {
		t.Fatalf("prompt=%q", resolution.Prompt)
	}
}

func TestVideoPromptUsesOrganizationTemplate(t *testing.T) {
	database := openPromptTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 3, Key: "storyboard_video", Name: "Custom Video", Category: "video",
		Content: "{{drama_title}} motion {{shot_title}} / {{image_prompt}}", VariablesJSON: `["drama_title","shot_title","image_prompt"]`,
		Version: 2, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{Title: "推镜", ImagePrompt: "rain", VideoPrompt: "slow dolly"}
	resolution := VideoPrompt(database, 3, models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}, storyboard, "", nil, nil)
	if resolution.Source != "organization_template" || resolution.Prompt != "归途 motion 推镜 / rain" {
		t.Fatalf("resolution=%+v", resolution)
	}
}

func TestFramePromptRejectsEmptyShotContent(t *testing.T) {
	database := openPromptTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 7, Key: "storyboard_image", Name: "Custom Image", Category: "image",
		Content: "{{drama_title}} always", VariablesJSON: `["drama_title"]`, Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	resolution := FramePrompt(database, 7, models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}, models.Storyboard{}, "first_frame", "", nil, nil)
	if resolution.Prompt != "" || resolution.Source != "fallback" {
		t.Fatalf("resolution=%+v", resolution)
	}
}

func TestFramePromptUsesExplicitPromptOverTemplateVariables(t *testing.T) {
	database := openPromptTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 7, Key: "storyboard_image", Name: "Custom Image", Category: "image",
		Content: "{{shot_description}}", VariablesJSON: `["shot_description"]`, Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{Title: "站台", ImagePrompt: "stored prompt", Description: "stored description"}
	resolution := FramePrompt(database, 7, models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}, storyboard, "first_frame", "API override", nil, nil)
	if resolution.Prompt != "opening frame, API override" {
		t.Fatalf("prompt=%q", resolution.Prompt)
	}
}

func TestApplyTemplateRequiresMatchingCategory(t *testing.T) {
	database := openPromptTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 7, Key: "storyboard_image", Name: "Wrong", Category: "video",
		Content: "wrong category", VariablesJSON: `[]`, Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	resolution := ApplyTemplate(database, 7, "storyboard_image", "image", Context{}, "fallback text")
	if resolution.Source != "fallback" || resolution.Prompt != "fallback text" {
		t.Fatalf("resolution=%+v", resolution)
	}
}

func TestCharacterScenePropImagePromptsUseOrganizationTemplates(t *testing.T) {
	database := openPromptTestDB(t)
	if err := database.AutoMigrate(&models.Prop{}); err != nil {
		t.Fatal(err)
	}
	for _, template := range []models.PromptTemplate{
		{OrganizationID: 12, Key: "character_image", Name: "Char", Category: "image", Content: "角色::{{character_name}}::{{character_appearance}}", VariablesJSON: `["character_name","character_appearance"]`, Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now"},
		{OrganizationID: 12, Key: "scene_image", Name: "Scene", Category: "image", Content: "场景::{{scene_location}}::{{scene_prompt}}", VariablesJSON: `["scene_location","scene_prompt"]`, Version: 2, IsActive: true, CreatedAt: "now", UpdatedAt: "now"},
		{OrganizationID: 12, Key: "prop_image", Name: "Prop", Category: "image", Content: "道具::{{prop_name}}::{{prop_prompt}}", VariablesJSON: `["prop_name","prop_prompt"]`, Version: 3, IsActive: true, CreatedAt: "now", UpdatedAt: "now"},
	} {
		if err := database.Create(&template).Error; err != nil {
			t.Fatal(err)
		}
	}
	drama := models.Drama{Title: "归途"}
	episode := models.Episode{Title: "重逢"}
	character := models.Character{Name: "阿宁", Appearance: "白外套"}
	scene := models.Scene{Location: "站台", Time: "夜", Prompt: "雨夜路灯"}
	prop := models.Prop{Name: "旧提箱", Prompt: "皮革磨损"}

	charResolution := CharacterImagePrompt(database, 12, drama, episode, character, "")
	if charResolution.Source != "organization_template" || charResolution.Prompt != "角色::阿宁::白外套" {
		t.Fatalf("character=%+v", charResolution)
	}
	sceneResolution := SceneImagePrompt(database, 12, drama, episode, scene, "")
	if sceneResolution.Source != "organization_template" || sceneResolution.Prompt != "场景::站台::雨夜路灯" {
		t.Fatalf("scene=%+v", sceneResolution)
	}
	propResolution := PropImagePrompt(database, 12, drama, episode, prop, "API override")
	if propResolution.Source != "organization_template" || propResolution.Prompt != "道具::旧提箱::API override" {
		t.Fatalf("prop=%+v", propResolution)
	}
}

func TestAssetImagePromptsRejectEmptyContent(t *testing.T) {
	database := openPromptTestDB(t)
	if CharacterImagePrompt(database, 1, models.Drama{}, models.Episode{}, models.Character{}, "").Prompt != "" {
		t.Fatal("empty character should not invent prompt")
	}
	if SceneImagePrompt(database, 1, models.Drama{}, models.Episode{}, models.Scene{}, "").Prompt != "" {
		t.Fatal("empty scene should not invent prompt")
	}
	if PropImagePrompt(database, 1, models.Drama{}, models.Episode{}, models.Prop{}, "").Prompt != "" {
		t.Fatal("empty prop should not invent prompt")
	}
}
