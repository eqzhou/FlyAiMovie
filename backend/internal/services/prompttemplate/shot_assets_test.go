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

func openShotAssetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shotassets.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.PromptTemplate{}, &models.Drama{}, &models.Episode{}, &models.Storyboard{}, &models.Character{}, &models.Scene{}, &models.StoryboardCharacter{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func createShotCharacter(t *testing.T, database *gorm.DB, character models.Character) models.Character {
	t.Helper()
	character.CreatedAt, character.UpdatedAt = "now", "now"
	if err := database.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	return character
}

func createShotScene(t *testing.T, database *gorm.DB, scene models.Scene) models.Scene {
	t.Helper()
	scene.CreatedAt, scene.UpdatedAt = "now", "now"
	if err := database.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	return scene
}

// A storyboard with explicit character links must use exactly those characters,
// not every character belonging to the drama.
func TestShotAssetNamesPrefersStoryboardLinkedCharacters(t *testing.T) {
	database := openShotAssetTestDB(t)
	linked := createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "阿宁"})
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "不该出现的配角"})
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.StoryboardCharacter{OrganizationID: 7, StoryboardID: storyboard.ID, CharacterID: linked.ID}).Error; err != nil {
		t.Fatal(err)
	}

	characterNames, _ := ShotAssetNames(database, 7, 3, &storyboard)
	if len(characterNames) != 1 || characterNames[0] != "阿宁" {
		t.Fatalf("characterNames=%#v, want only the linked character", characterNames)
	}
}

// When the storyboard has no character links, the drama roster is the fallback.
func TestShotAssetNamesFallsBackToDramaRoster(t *testing.T) {
	database := openShotAssetTestDB(t)
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "阿宁"})
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "老周"})
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	characterNames, _ := ShotAssetNames(database, 7, 3, &storyboard)
	if len(characterNames) != 2 || characterNames[0] != "阿宁" || characterNames[1] != "老周" {
		t.Fatalf("characterNames=%#v, want the full drama roster in id order", characterNames)
	}
}

// Both the linked-character path and the drama fallback must stay organization scoped
// and must ignore soft-deleted rows.
func TestShotAssetNamesExcludesOtherOrganizationsAndDeletedRows(t *testing.T) {
	database := openShotAssetTestDB(t)
	deletedAt := "2026-01-01"
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "已删除", DeletedAt: &deletedAt})
	createShotCharacter(t, database, models.Character{OrganizationID: 8, DramaID: 3, Name: "他组角色"})
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "阿宁"})
	createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "已删除场景", DeletedAt: &deletedAt})
	createShotScene(t, database, models.Scene{OrganizationID: 8, DramaID: 3, Location: "他组场景"})
	createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "站台", Time: "夜"})

	characterNames, sceneNames := ShotAssetNames(database, 7, 3, nil)
	if len(characterNames) != 1 || characterNames[0] != "阿宁" {
		t.Fatalf("characterNames=%#v, want only the live in-organization character", characterNames)
	}
	if len(sceneNames) != 1 || sceneNames[0] != "站台·夜" {
		t.Fatalf("sceneNames=%#v, want only the live in-organization scene", sceneNames)
	}
}

// A storyboard pinned to a scene resolves that scene rather than the drama scene list,
// and the label joins location and time.
func TestShotAssetNamesUsesStoryboardSceneAndLabelsTime(t *testing.T) {
	database := openShotAssetTestDB(t)
	pinned := createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "天台", Time: "黄昏"})
	createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "不该出现的场景", Time: "日"})
	sceneID := pinned.ID
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, SceneID: &sceneID, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	_, sceneNames := ShotAssetNames(database, 7, 3, &storyboard)
	if len(sceneNames) != 1 || sceneNames[0] != "天台·黄昏" {
		t.Fatalf("sceneNames=%#v, want only the pinned scene labelled with its time", sceneNames)
	}
}

// A scene without a time must not gain a dangling separator, and a storyboard whose
// pinned scene belongs to another organization falls back to the drama scene list.
func TestShotAssetNamesOmitsEmptyTimeAndFallsBackForForeignScene(t *testing.T) {
	database := openShotAssetTestDB(t)
	foreign := createShotScene(t, database, models.Scene{OrganizationID: 8, DramaID: 3, Location: "他组场景", Time: "夜"})
	createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "巷口"})
	sceneID := foreign.ID
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, SceneID: &sceneID, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	_, sceneNames := ShotAssetNames(database, 7, 3, &storyboard)
	if len(sceneNames) != 1 || sceneNames[0] != "巷口" {
		t.Fatalf("sceneNames=%#v, want the drama fallback scene with no time separator", sceneNames)
	}
}

// Without a drama id and without a storyboard there is nothing to resolve; the helper
// must return empty slices instead of every organization's rows.
func TestShotAssetNamesReturnsEmptyWithoutDramaOrStoryboard(t *testing.T) {
	database := openShotAssetTestDB(t)
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "阿宁"})
	createShotScene(t, database, models.Scene{OrganizationID: 7, DramaID: 3, Location: "站台"})

	characterNames, sceneNames := ShotAssetNames(database, 7, 0, nil)
	if len(characterNames) != 0 || len(sceneNames) != 0 {
		t.Fatalf("characterNames=%#v sceneNames=%#v, want both empty", characterNames, sceneNames)
	}
}

// Linked rows pointing at characters from another organization must not leak through
// the id IN (...) lookup.
func TestShotAssetNamesIgnoresLinksToForeignCharacters(t *testing.T) {
	database := openShotAssetTestDB(t)
	foreign := createShotCharacter(t, database, models.Character{OrganizationID: 8, DramaID: 3, Name: "他组角色"})
	createShotCharacter(t, database, models.Character{OrganizationID: 7, DramaID: 3, Name: "阿宁"})
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, CreatedAt: "now", UpdatedAt: "now"}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.StoryboardCharacter{OrganizationID: 7, StoryboardID: storyboard.ID, CharacterID: foreign.ID}).Error; err != nil {
		t.Fatal(err)
	}

	characterNames, _ := ShotAssetNames(database, 7, 3, &storyboard)
	for _, name := range characterNames {
		if name == "他组角色" {
			t.Fatalf("characterNames=%#v leaked a character from another organization", characterNames)
		}
	}
	if len(characterNames) != 1 || characterNames[0] != "阿宁" {
		t.Fatalf("characterNames=%#v, want the drama fallback", characterNames)
	}
}

// grid_rows/grid_cols render as empty strings for non-positive values so templates do
// not print "0" or negative dimensions.
func TestIntStringSuppressesNonPositiveValues(t *testing.T) {
	if got := intString(0); got != "" {
		t.Fatalf("intString(0)=%q, want empty", got)
	}
	if got := intString(-3); got != "" {
		t.Fatalf("intString(-3)=%q, want empty", got)
	}
	if got := intString(4); got != "4" {
		t.Fatalf("intString(4)=%q, want 4", got)
	}
}

// GridPrompt must clamp non-positive dimensions to 2x2 and pad missing shots with
// placeholder panels rather than producing fewer cells than the grid.
func TestGridPromptClampsDimensionsAndPadsPanels(t *testing.T) {
	database := openShotAssetTestDB(t)
	shots := []models.Storyboard{{Title: "开场", ImagePrompt: "rain station"}}

	resolution, cells := GridPrompt(database, 9, models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}, "multi_ref", 0, -1, shots, nil, nil)
	if len(cells) != 4 {
		t.Fatalf("cells=%#v, want 4 panels after clamping to 2x2", cells)
	}
	if cells[0] != "rain station" {
		t.Fatalf("cells[0]=%q, want the shot prompt", cells[0])
	}
	for _, cell := range cells[1:] {
		if cell != "empty cinematic panel, dark, no text" {
			t.Fatalf("cells=%#v, want padded placeholders", cells)
		}
	}
	if !strings.Contains(resolution.Prompt, "2x2 storyboard grid with exactly 4 equal panels") {
		t.Fatalf("prompt=%q, want clamped 2x2 header", resolution.Prompt)
	}
	if !strings.Contains(resolution.Prompt, "Maintain character identity consistency across panels.") {
		t.Fatalf("prompt=%q, want multi_ref guidance", resolution.Prompt)
	}
}

// A shot with no usable text still needs a non-empty panel description.
func TestGridPromptSubstitutesPlaceholderForEmptyShot(t *testing.T) {
	database := openShotAssetTestDB(t)
	shots := []models.Storyboard{{}, {Action: "转身离开"}}

	resolution, cells := GridPrompt(database, 9, models.Drama{}, models.Episode{}, "first_last", 1, 2, shots, nil, nil)
	if len(cells) != 2 || cells[0] != "cinematic shot" || cells[1] != "转身离开" {
		t.Fatalf("cells=%#v, want a placeholder for the empty shot and the action for the second", cells)
	}
	if !strings.Contains(resolution.Prompt, "opening frame first, ending frame second") {
		t.Fatalf("prompt=%q, want first_last guidance", resolution.Prompt)
	}
}

// The grid template receives the clamped dimensions and mode through approved variables.
func TestGridPromptTemplateReceivesGridVariables(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 9, Key: "grid_composition", Name: "Grid", Category: "grid",
		Content:       "{{grid_rows}}x{{grid_cols}} mode={{grid_mode}} :: {{user_instruction}}",
		VariablesJSON: `["grid_rows","grid_cols","grid_mode","user_instruction"]`,
		Version:       5, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	shots := []models.Storyboard{{ImagePrompt: "rain"}, {ImagePrompt: "neon"}}

	resolution, _ := GridPrompt(database, 9, models.Drama{}, models.Episode{}, "first_last", 1, 2, shots, nil, nil)
	if resolution.Source != "organization_template" || resolution.Version != 5 {
		t.Fatalf("resolution=%+v, want the organization grid template", resolution)
	}
	if resolution.Prompt != "1x2 mode=first_last :: rain; neon" {
		t.Fatalf("prompt=%q", resolution.Prompt)
	}
}

// VideoPrompt falls back through video -> image -> description -> action -> title, and
// an explicit prompt overrides the stored shot text.
func TestVideoPromptInstructionFallbackChain(t *testing.T) {
	database := openShotAssetTestDB(t)
	drama, episode := models.Drama{Title: "归途"}, models.Episode{Title: "重逢"}

	tests := []struct {
		name       string
		storyboard models.Storyboard
		explicit   string
		want       string
	}{
		{name: "video prompt wins", storyboard: models.Storyboard{Title: "推镜", VideoPrompt: "slow dolly", ImagePrompt: "rain"}, want: "slow dolly"},
		{name: "image prompt next", storyboard: models.Storyboard{Title: "推镜", ImagePrompt: "rain", Description: "两人相遇"}, want: "rain"},
		{name: "description next", storyboard: models.Storyboard{Title: "推镜", Description: "两人相遇", Action: "转身"}, want: "两人相遇"},
		{name: "action next", storyboard: models.Storyboard{Title: "推镜", Action: "转身"}, want: "转身"},
		{name: "title last", storyboard: models.Storyboard{Title: "推镜"}, want: "推镜"},
		{name: "explicit overrides all", storyboard: models.Storyboard{Title: "推镜", VideoPrompt: "slow dolly"}, explicit: "  API override  ", want: "API override"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolution := VideoPrompt(database, 3, drama, episode, tc.storyboard, tc.explicit, nil, nil)
			if resolution.Source != "fallback" || resolution.Prompt != tc.want {
				t.Fatalf("resolution=%+v, want prompt %q", resolution, tc.want)
			}
		})
	}
}

// A blank shot must not invent a video prompt.
func TestVideoPromptRejectsEmptyShotContent(t *testing.T) {
	database := openShotAssetTestDB(t)
	resolution := VideoPrompt(database, 3, models.Drama{Title: "归途"}, models.Episode{}, models.Storyboard{}, "   ", nil, nil)
	if resolution.Prompt != "" || resolution.Source != "fallback" || resolution.Key != "storyboard_video" {
		t.Fatalf("resolution=%+v, want an empty storyboard_video fallback", resolution)
	}
}

// An explicit prompt must reach the template through video_prompt, proving the override
// is applied to the shot copy and not only to user_instruction.
func TestVideoPromptExplicitOverrideReachesTemplateVariables(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 3, Key: "storyboard_video", Name: "Video", Category: "video",
		Content: "{{video_prompt}} | {{shot_description}}", VariablesJSON: `["video_prompt","shot_description"]`,
		Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{Title: "推镜", VideoPrompt: "stored video", Description: "stored description"}

	resolution := VideoPrompt(database, 3, models.Drama{}, models.Episode{}, storyboard, "API override", nil, nil)
	if resolution.Prompt != "API override | API override" {
		t.Fatalf("prompt=%q, want the explicit override in both variables", resolution.Prompt)
	}
}

// A template that renders to only whitespace must not replace the fallback text.
func TestApplyTemplateIgnoresWhitespaceOnlyRender(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 4, Key: "storyboard_image", Name: "Blank", Category: "image",
		Content: "  {{user_instruction}}  ", VariablesJSON: `["user_instruction"]`,
		Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}

	resolution := ApplyTemplate(database, 4, "storyboard_image", "image", Context{UserInstruction: "   "}, "fallback text")
	if resolution.Source != "fallback" || resolution.Prompt != "fallback text" {
		t.Fatalf("resolution=%+v, want the fallback when the render is blank", resolution)
	}
}

// A stored template containing a variable outside the whitelist must fail closed to the
// fallback instead of emitting the raw token.
func TestApplyTemplateFallsBackWhenStoredContentIsInvalid(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 4, Key: "storyboard_image", Name: "Bad", Category: "image",
		Content: "{{unknown_variable}}", VariablesJSON: `[]`,
		Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}

	resolution := ApplyTemplate(database, 4, "storyboard_image", "image", Context{}, "fallback text")
	if resolution.Source != "fallback" || resolution.Prompt != "fallback text" {
		t.Fatalf("resolution=%+v, want the fallback for an unrenderable template", resolution)
	}
}

// Inactive templates must be ignored.
func TestApplyTemplateIgnoresInactiveTemplate(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 4, Key: "storyboard_image", Name: "Inactive", Category: "image",
		Content: "custom", VariablesJSON: `[]`,
		Version: 1, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	// is_active has a `default:true` column default, so inserting the zero value would
	// be overwritten by GORM; deactivate explicitly instead.
	if err := database.Model(&models.PromptTemplate{}).Where("id = ?", template.ID).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}

	resolution := ApplyTemplate(database, 4, "storyboard_image", "image", Context{}, "fallback text")
	if resolution.Source != "fallback" || resolution.Prompt != "fallback text" {
		t.Fatalf("resolution=%+v, want inactive templates ignored", resolution)
	}
}

// An empty category matches a template regardless of its category.
func TestApplyTemplateWithoutCategoryMatchesAnyCategory(t *testing.T) {
	database := openShotAssetTestDB(t)
	template := models.PromptTemplate{
		OrganizationID: 4, Key: "storyboard_image", Name: "Any", Category: "video",
		Content: "matched", VariablesJSON: `[]`,
		Version: 2, IsActive: true, CreatedAt: "now", UpdatedAt: "now",
	}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}

	resolution := ApplyTemplate(database, 4, "storyboard_image", "  ", Context{}, "fallback text")
	if resolution.Source != "organization_template" || resolution.Prompt != "matched" || resolution.Version != 2 {
		t.Fatalf("resolution=%+v, want the template matched without a category filter", resolution)
	}
}
