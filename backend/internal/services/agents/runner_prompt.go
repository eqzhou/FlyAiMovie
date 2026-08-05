package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/eqzhou/flyaimovie/internal/services/skillregistry"
)

func (r *Runner) buildSystemPrompt(agentType string) string {
	base := defaultPrompt(agentType)
	skill := r.loadSkill(agentType)
	tools := toolCatalog(agentType)
	return base + "\n\n" + skill + "\n\n" + tools
}

func (r *Runner) resolveSystemPrompt(organizationID uint, agentType string, dramaID uint, episode models.Episode, message, legacyPrompt string) (PromptResolution, error) {
	return r.resolveSystemPromptWithOptions(organizationID, agentType, dramaID, episode, message, legacyPrompt, RunOptions{})
}

func (r *Runner) resolveSystemPromptWithOptions(organizationID uint, agentType string, dramaID uint, episode models.Episode, message, legacyPrompt string, options RunOptions) (PromptResolution, error) {
	base := defaultPrompt(agentType)
	resolution := PromptResolution{Source: "builtin", Key: agentType}
	version := 0
	var template models.PromptTemplate
	if err := db.DB.Where("organization_id = ? AND key = ? AND category = ? AND is_active = ? AND deleted_at IS NULL", organizationID, agentType, "agent_system", true).First(&template).Error; err == nil {
		var drama models.Drama
		_ = db.DB.Where("organization_id = ? AND id = ?", organizationID, dramaID).First(&drama).Error
		var characters []models.Character
		var scenes []models.Scene
		_ = db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&characters).Error
		_ = db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&scenes).Error
		characterNames := make([]string, 0, len(characters))
		for _, character := range characters {
			characterNames = append(characterNames, character.Name)
		}
		sceneNames := make([]string, 0, len(scenes))
		for _, scene := range scenes {
			sceneNames = append(sceneNames, scene.Location)
		}
		if rendered, err := prompttemplate.Render(template.Content, map[string]string{
			"drama_title": drama.Title, "episode_title": episode.Title, "user_instruction": message,
			"character_names": strings.Join(characterNames, "、"), "scene_names": strings.Join(sceneNames, "、"),
		}); err == nil {
			base = rendered
			version = template.Version
			resolution.Source = "organization_template"
			resolution.TemplateID = template.ID
			resolution.Key = template.Key
			resolution.Version = template.Version
		}
	} else if strings.TrimSpace(legacyPrompt) != "" {
		base = legacyPrompt
		resolution.Source = "agent_config"
	}
	if version > 0 {
		base += fmt.Sprintf("\n\n提示词模板版本: %d", version)
	}
	skillText := ""
	if override := options.SkillSnapshot; override != nil {
		if override.SkillSnapshot == "" {
			return PromptResolution{}, fmt.Errorf("source run skill snapshot is empty")
		}
		digest := sha256.Sum256([]byte(override.SkillSnapshot))
		computedHash := hex.EncodeToString(digest[:])
		if override.SkillHash != "" && !strings.EqualFold(override.SkillHash, computedHash) {
			return PromptResolution{}, fmt.Errorf("source run skill snapshot hash mismatch")
		}
		skillText = override.SkillSnapshot
		resolution.SkillSource = override.SkillSource
		if resolution.SkillSource == "" {
			resolution.SkillSource = "builtin"
		}
		resolution.SkillID = override.SkillID
		resolution.SkillVersionID = override.SkillVersionID
		resolution.SkillVersion = override.SkillVersion
		resolution.SkillHash = computedHash
		resolution.SkillSnapshotMode = "source_run"
		resolution.SkillSnapshotSourceRunID = override.SourceRunID
	} else {
		resolution.SkillSource = "builtin"
	}
	if options.SkillSnapshot == nil {
		published, resolveErr := skillregistry.New(db.DB).ResolvePublished(organizationID, agentType)
		switch {
		case resolveErr == nil:
			rendered, renderErr := skillregistry.RenderVersion(*published)
			if renderErr != nil {
				return PromptResolution{}, fmt.Errorf("render published skill: %w", renderErr)
			}
			skillText = rendered
			resolution.SkillSource = "database"
			resolution.SkillID = published.SkillID
			resolution.SkillVersionID = published.ID
			resolution.SkillVersion = published.Version
			digest := sha256.Sum256([]byte(rendered))
			resolution.SkillHash = hex.EncodeToString(digest[:])
		case errors.Is(resolveErr, skillregistry.ErrNotFound):
			// An absent or unpublished organization skill deliberately uses the
			// built-in skill snapshot.
		default:
			return PromptResolution{}, fmt.Errorf("resolve published skill: %w", resolveErr)
		}
	}
	if options.SkillSnapshot == nil && resolution.SkillSource == "builtin" {
		skillText = r.loadSkill(agentType)
		digest := sha256.Sum256([]byte(skillText))
		resolution.SkillHash = hex.EncodeToString(digest[:])
	}
	resolution.SkillSnapshot = skillText
	resolution.System = base + "\n\n" + skillText + "\n\n" + toolCatalog(agentType)
	return resolution, nil
}

func (r *Runner) loadSkill(agentType string) string {
	p := filepath.Join(r.SkillsDir, agentType, "SKILL.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	content := string(b)
	if strings.HasPrefix(content, "---") {
		if i := strings.Index(content[3:], "\n---"); i >= 0 {
			content = strings.TrimSpace(content[3+i+4:])
		}
	}
	sections := []string{strings.TrimSpace(content)}
	refDir := filepath.Join(r.SkillsDir, agentType, "reference")
	entries, err := os.ReadDir(refDir)
	if err == nil {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			body, readErr := os.ReadFile(filepath.Join(refDir, entry.Name()))
			if readErr != nil {
				continue
			}
			trimmed := strings.TrimSpace(string(body))
			if trimmed == "" {
				continue
			}
			sections = append(sections, "### "+strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))+"\n"+trimmed)
		}
	}
	return "## Skill\n" + strings.Join(sections, "\n\n")
}

func defaultPrompt(agentType string) string {
	switch agentType {
	case "script_rewriter":
		return "你是专业编剧，将小说/大纲改写为格式化短剧剧本。场景头：## S编号 | 内景/外景 · 地点 | 时间段。对白：角色名：（状态）台词。"
	case "extractor":
		return "你是制片助理，从剧本提取角色与场景，按名字/地点+时间段去重，并关联到当前集。"
	case "storyboard_breaker":
		return "你是分镜师，将剧本拆为 10-15 秒镜头，补全 shot_type/angle/movement/action/dialogue/image_prompt/video_prompt/duration 等字段。"
	case "voice_assigner":
		return "你是配音导演，根据角色性别年龄性格分配音色。"
	case "grid_prompt_generator":
		return "你是图像提示词工程师，为角色/场景/宫格图生成英文提示词，强调 consistent art style 与 cinematic quality。"
	default:
		return "你是短剧制作助手。"
	}
}

func toolCatalog(agentType string) string {
	switch agentType {
	case "script_rewriter":
		return `可用工具: read_episode_script, save_script`
	case "extractor":
		return `可用工具: read_script_for_extraction, read_existing_characters, read_existing_scenes, save_dedup_characters, save_dedup_scenes
save_dedup_characters args: {"characters":[{"name":"","role":"","appearance":"","personality":"","description":""}]}
save_dedup_scenes args: {"scenes":[{"location":"","time":"","prompt":""}]}`
	case "storyboard_breaker":
		return `可用工具: read_storyboard_context, save_storyboards
save_storyboards args: {"storyboards":[{"title":"","shot_type":"","angle":"","movement":"","location":"","time":"","action":"","dialogue":"","description":"","result":"","atmosphere":"","image_prompt":"","video_prompt":"","bgm_prompt":"","sound_effect":"","duration":12,"scene_id":null,"character_ids":[]}]}`
	case "voice_assigner":
		return `可用工具: list_voices, get_characters, assign_voice
assign_voice args: {"character_id":1,"voice_id":"xxx","voice_provider":"minimax"}`
	case "grid_prompt_generator":
		return `可用工具: read_characters, read_scenes, read_shots_for_grid, generate_grid_prompt
generate_grid_prompt args: {"mode":"first_frame","rows":2,"cols":2,"grid_prompt":"...","cell_prompts":["..."]}`
	default:
		return ""
	}
}

func (r *Runner) defaultActions(agentType string, episodeID, dramaID uint) []AgentAction {
	// deterministic fallback pipeline steps when model JSON fails
	switch agentType {
	case "script_rewriter":
		return []AgentAction{{Tool: "read_episode_script", Args: map[string]any{}}}
	case "extractor":
		return []AgentAction{
			{Tool: "read_script_for_extraction", Args: map[string]any{}},
			{Tool: "read_existing_characters", Args: map[string]any{}},
			{Tool: "read_existing_scenes", Args: map[string]any{}},
		}
	default:
		return nil
	}
}
