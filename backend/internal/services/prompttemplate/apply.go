package prompttemplate

import (
	"fmt"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/textutil"
	"gorm.io/gorm"
)

// Context gathers organization-scoped values for rendering built-in and custom prompt templates.
type Context struct {
	OrganizationID  uint
	Drama           models.Drama
	Episode         models.Episode
	Storyboard      models.Storyboard
	Character       models.Character
	Scene           models.Scene
	Prop            models.Prop
	CharacterNames  []string
	SceneNames      []string
	UserInstruction string
	GridRows        int
	GridCols        int
	GridMode        string
}

// Resolution records which template (if any) was used for a generation prompt.
type Resolution struct {
	Source     string
	TemplateID uint
	Key        string
	Version    int
	Prompt     string
}

func joinNames(values []string) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return strings.Join(cleaned, "、")
}

func (ctx Context) values() map[string]string {
	return map[string]string{
		"drama_title":           ctx.Drama.Title,
		"episode_title":         ctx.Episode.Title,
		"user_instruction":      ctx.UserInstruction,
		"character_names":       joinNames(ctx.CharacterNames),
		"scene_names":           joinNames(ctx.SceneNames),
		"shot_title":            ctx.Storyboard.Title,
		"shot_description":      textutil.FirstNonBlank(ctx.Storyboard.Description, ctx.Storyboard.Action, ctx.Storyboard.Title),
		"image_prompt":          textutil.FirstNonBlank(ctx.Storyboard.ImagePrompt, ctx.Storyboard.Description, ctx.Storyboard.Action),
		"video_prompt":          textutil.FirstNonBlank(ctx.Storyboard.VideoPrompt, ctx.Storyboard.ImagePrompt, ctx.Storyboard.Description),
		"grid_rows":             intString(ctx.GridRows),
		"grid_cols":             intString(ctx.GridCols),
		"grid_mode":             ctx.GridMode,
		"character_name":        ctx.Character.Name,
		"character_role":        textutil.FirstNonBlank(ctx.Character.Role, "角色"),
		"character_appearance":  textutil.FirstNonBlank(ctx.Character.Appearance, ctx.Character.Description, ctx.Character.Name),
		"character_description": ctx.Character.Description,
		"character_personality": ctx.Character.Personality,
		"scene_location":        ctx.Scene.Location,
		"scene_time":            textutil.FirstNonBlank(ctx.Scene.Time, "日"),
		"scene_prompt":          textutil.FirstNonBlank(ctx.Scene.Prompt, ctx.Scene.Location),
		"prop_name":             ctx.Prop.Name,
		"prop_type":             textutil.FirstNonBlank(ctx.Prop.Type, "道具"),
		"prop_description":      ctx.Prop.Description,
		"prop_prompt":           textutil.FirstNonBlank(ctx.Prop.Prompt, ctx.Prop.Name),
	}
}

func intString(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

// ShotAssetNames loads character and scene labels for a drama or a specific storyboard.
func ShotAssetNames(database *gorm.DB, organizationID, dramaID uint, storyboard *models.Storyboard) ([]string, []string) {
	var characters []models.Character
	if storyboard != nil {
		var links []models.StoryboardCharacter
		_ = database.Where("storyboard_id = ?", storyboard.ID).Find(&links).Error
		ids := make([]uint, 0, len(links))
		for _, link := range links {
			ids = append(ids, link.CharacterID)
		}
		if len(ids) > 0 {
			_ = database.Where("organization_id = ? AND id IN ? AND deleted_at IS NULL", organizationID, ids).Order("id").Find(&characters).Error
		}
	}
	if len(characters) == 0 && dramaID > 0 {
		_ = database.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&characters).Error
	}
	characterNames := make([]string, 0, len(characters))
	for _, character := range characters {
		characterNames = append(characterNames, character.Name)
	}
	var scenes []models.Scene
	if storyboard != nil && storyboard.SceneID != nil {
		var scene models.Scene
		if err := database.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, *storyboard.SceneID).First(&scene).Error; err == nil {
			scenes = append(scenes, scene)
		}
	}
	if len(scenes) == 0 && dramaID > 0 {
		_ = database.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&scenes).Error
	}
	sceneNames := make([]string, 0, len(scenes))
	for _, scene := range scenes {
		label := scene.Location
		if strings.TrimSpace(scene.Time) != "" {
			label += "·" + scene.Time
		}
		sceneNames = append(sceneNames, label)
	}
	return characterNames, sceneNames
}

// ApplyTemplate renders the active organization template for key+category, falling back when absent.
func ApplyTemplate(database *gorm.DB, organizationID uint, key, category string, context Context, fallback string) Resolution {
	resolution := Resolution{Source: "fallback", Key: key, Prompt: strings.TrimSpace(fallback)}
	var template models.PromptTemplate
	query := database.Where("organization_id = ? AND key = ? AND is_active = ? AND deleted_at IS NULL", organizationID, key, true)
	if strings.TrimSpace(category) != "" {
		query = query.Where("category = ?", category)
	}
	if err := query.First(&template).Error; err != nil {
		return resolution
	}
	rendered, renderErr := Render(template.Content, context.values())
	if renderErr != nil {
		return resolution
	}
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return resolution
	}
	return Resolution{
		Source:     "organization_template",
		TemplateID: template.ID,
		Key:        template.Key,
		Version:    template.Version,
		Prompt:     rendered,
	}
}

// FramePrompt builds the image prompt for a storyboard frame generation request.
// explicitPrompt overrides the shot image/description variables when provided.
func FramePrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, storyboard models.Storyboard, frameType, explicitPrompt string, characterNames, sceneNames []string) Resolution {
	explicit := strings.TrimSpace(explicitPrompt)
	instruction := textutil.FirstNonBlank(explicit, storyboard.ImagePrompt, storyboard.Description, storyboard.Action, storyboard.Title)
	if strings.TrimSpace(instruction) == "" {
		return Resolution{Source: "fallback", Key: "storyboard_image"}
	}
	shot := storyboard
	if explicit != "" {
		shot.ImagePrompt = explicit
		shot.Description = explicit
	}
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode, Storyboard: shot,
		CharacterNames: characterNames, SceneNames: sceneNames, UserInstruction: instruction,
	}
	resolution := ApplyTemplate(database, organizationID, "storyboard_image", "image", context, instruction)
	switch frameType {
	case "last_frame":
		resolution.Prompt = "ending frame, " + resolution.Prompt
	case "composed", "storyboard":
		resolution.Prompt = "storyboard panel composition, " + resolution.Prompt
	default:
		resolution.Prompt = "opening frame, " + resolution.Prompt
	}
	return resolution
}

// VideoPrompt builds the video generation prompt for a storyboard.
// explicitPrompt overrides the shot video/image variables when provided.
func VideoPrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, storyboard models.Storyboard, explicitPrompt string, characterNames, sceneNames []string) Resolution {
	explicit := strings.TrimSpace(explicitPrompt)
	instruction := textutil.FirstNonBlank(explicit, storyboard.VideoPrompt, storyboard.ImagePrompt, storyboard.Description, storyboard.Action, storyboard.Title)
	if strings.TrimSpace(instruction) == "" {
		return Resolution{Source: "fallback", Key: "storyboard_video"}
	}
	shot := storyboard
	if explicit != "" {
		shot.VideoPrompt = explicit
		shot.Description = explicit
	}
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode, Storyboard: shot,
		CharacterNames: characterNames, SceneNames: sceneNames, UserInstruction: instruction,
	}
	return ApplyTemplate(database, organizationID, "storyboard_video", "video", context, instruction)
}

// GridPrompt builds a multi-panel composition prompt and per-cell fallbacks.
func GridPrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, mode string, rows, cols int, shots []models.Storyboard, characterNames, sceneNames []string) (Resolution, []string) {
	if rows <= 0 {
		rows = 2
	}
	if cols <= 0 {
		cols = 2
	}
	cells := make([]string, 0, rows*cols)
	for i := 0; i < rows*cols; i++ {
		if i < len(shots) {
			sb := shots[i]
			p := textutil.FirstNonBlank(sb.ImagePrompt, sb.Description, sb.Action, sb.Title)
			if p == "" {
				p = "cinematic shot"
			}
			cells = append(cells, p)
		} else {
			cells = append(cells, "empty cinematic panel, dark, no text")
		}
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Create a seamless %dx%d storyboard grid with exactly %d equal panels, consistent art style, cinematic lighting, high detail, no text, no watermark, no borders labels.\n", rows, cols, rows*cols))
	switch mode {
	case "first_last":
		builder.WriteString("For each shot, create two panels in order: opening frame first, ending frame second. Keep the same characters and composition across the pair.\n")
	case "multi_ref":
		builder.WriteString("Maintain character identity consistency across panels.\n")
	default:
		builder.WriteString("Each panel is a first-frame composition.\n")
	}
	for i, cell := range cells {
		builder.WriteString(fmt.Sprintf("Panel %d: %s\n", i+1, cell))
	}
	fallback := builder.String()
	instruction := strings.TrimSpace(strings.Join(cells, "; "))
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode,
		CharacterNames: characterNames, SceneNames: sceneNames, UserInstruction: instruction,
		GridRows: rows, GridCols: cols, GridMode: mode,
	}
	return ApplyTemplate(database, organizationID, "grid_composition", "grid", context, fallback), cells
}

// CharacterImagePrompt builds the image prompt for a character portrait generation.
// explicitPrompt overrides appearance/description variables when provided.
func CharacterImagePrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, character models.Character, explicitPrompt string) Resolution {
	explicit := strings.TrimSpace(explicitPrompt)
	instruction := textutil.FirstNonBlank(explicit, character.Appearance, character.Description, character.Name)
	if strings.TrimSpace(instruction) == "" {
		return Resolution{Source: "fallback", Key: "character_image"}
	}
	shotCharacter := character
	if explicit != "" {
		shotCharacter.Appearance = explicit
		shotCharacter.Description = explicit
	}
	fallback := strings.TrimSpace(shotCharacter.Name + ", " + textutil.FirstNonBlank(shotCharacter.Appearance, shotCharacter.Description, "人物立绘") + ", high quality, front view, white background")
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode, Character: shotCharacter,
		CharacterNames: []string{shotCharacter.Name}, UserInstruction: instruction,
	}
	return ApplyTemplate(database, organizationID, "character_image", "image", context, fallback)
}

// SceneImagePrompt builds the image prompt for a scene environment generation.
func SceneImagePrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, scene models.Scene, explicitPrompt string) Resolution {
	explicit := strings.TrimSpace(explicitPrompt)
	instruction := textutil.FirstNonBlank(explicit, scene.Prompt, scene.Location)
	if strings.TrimSpace(instruction) == "" {
		return Resolution{Source: "fallback", Key: "scene_image"}
	}
	shotScene := scene
	if explicit != "" {
		shotScene.Prompt = explicit
	}
	fallback := textutil.FirstNonBlank(shotScene.Prompt, strings.TrimSpace(shotScene.Location+", "+textutil.FirstNonBlank(shotScene.Time, "日")+", cinematic scene, high quality"))
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode, Scene: shotScene,
		SceneNames: []string{textutil.FirstNonBlank(shotScene.Location, "场景")}, UserInstruction: instruction,
	}
	return ApplyTemplate(database, organizationID, "scene_image", "image", context, fallback)
}

// PropImagePrompt builds the image prompt for a prop still generation.
func PropImagePrompt(database *gorm.DB, organizationID uint, drama models.Drama, episode models.Episode, prop models.Prop, explicitPrompt string) Resolution {
	explicit := strings.TrimSpace(explicitPrompt)
	instruction := textutil.FirstNonBlank(explicit, prop.Prompt, prop.Description, prop.Name)
	if strings.TrimSpace(instruction) == "" {
		return Resolution{Source: "fallback", Key: "prop_image"}
	}
	shotProp := prop
	if explicit != "" {
		shotProp.Prompt = explicit
		shotProp.Description = explicit
	}
	fallback := textutil.FirstNonBlank(shotProp.Prompt, shotProp.Name+", prop, product photography")
	context := Context{
		OrganizationID: organizationID, Drama: drama, Episode: episode, Prop: shotProp,
		UserInstruction: instruction,
	}
	return ApplyTemplate(database, organizationID, "prop_image", "image", context, fallback)
}
