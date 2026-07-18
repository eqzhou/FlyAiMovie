package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"gorm.io/gorm"
)

var ValidAgentTypes = []string{
	"script_rewriter",
	"extractor",
	"storyboard_breaker",
	"voice_assigner",
	"grid_prompt_generator",
}

type ChatResult struct {
	Type        string           `json:"type"`
	Text        string           `json:"text"`
	ToolCalls   []map[string]any `json:"toolCalls"`
	ToolResults []map[string]any `json:"toolResults"`
}

type Runner struct {
	SkillsDir string
}

func NewRunner(skillsDir string) *Runner {
	return &Runner{SkillsDir: skillsDir}
}

func (r *Runner) IsValid(t string) bool {
	for _, v := range ValidAgentTypes {
		if v == t {
			return true
		}
	}
	return false
}

func (r *Runner) Run(ctx context.Context, organizationID uint, agentType string, dramaID, episodeID uint, message string) (*ChatResult, error) {
	if !r.IsValid(agentType) {
		return nil, fmt.Errorf("unsupported agent type %q", agentType)
	}
	var requestedEpisode models.Episode
	if err := db.DB.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, episodeID, dramaID).First(&requestedEpisode).Error; err != nil {
		return nil, fmt.Errorf("episode does not belong to drama")
	}
	var textConfig models.AIServiceConfig
	configErr := db.DB.Where("organization_id = ? AND service_type = ? AND is_active = ?", organizationID, "text", true).Order("is_default desc, priority desc, id desc").First(&textConfig).Error
	var cfg *ai.ServiceConfig
	var err error
	if configErr == nil {
		cfg, err = ai.GetOrganizationConfig(organizationID, "text", &textConfig.ID)
	} else {
		err = configErr
	}
	if err != nil {
		if res, ok := r.offlineFallback(organizationID, agentType, dramaID, episodeID, message, err); ok {
			return res, nil
		}
		return nil, err
	}
	var agentConfig models.AgentConfig
	if configErr := db.DB.Where("organization_id = ? AND agent_type = ? AND is_active = ? AND deleted_at IS NULL", organizationID, agentType, true).Order("id desc").First(&agentConfig).Error; configErr == nil {
		if agentConfig.Model != "" {
			cfg.Model = agentConfig.Model
		}
	}

	system := r.resolveSystemPrompt(organizationID, agentType, dramaID, requestedEpisode, message, agentConfig.SystemPrompt)
	temperature := float32(0.4)
	if agentConfig.Temperature != nil {
		temperature = float32(*agentConfig.Temperature)
	}
	maxTokens := 0
	if agentConfig.MaxTokens != nil && *agentConfig.MaxTokens > 0 {
		maxTokens = *agentConfig.MaxTokens
	}
	maxIterations := 2
	if agentConfig.MaxIterations != nil && *agentConfig.MaxIterations > 0 {
		maxIterations = *agentConfig.MaxIterations
	}
	// Phase 1: model produces structured actions (JSON) then we execute tools server-side.
	user := fmt.Sprintf(`项目ID=%d 集ID=%d
用户指令：%s

请严格按 JSON 输出（不要 markdown 代码块）：
{
  "actions": [
    {"tool":"工具名","args":{...}}
  ],
  "summary": "给用户的中文总结"
}

可用工具取决于 agent 类型。`, dramaID, episodeID, message)

	raw, err := ai.ChatWithMaxTokens(ctx, cfg, system, user, temperature, maxTokens)
	if err != nil {
		// Offline / misconfigured text model: deterministic fallbacks
		if res, ok := r.offlineFallback(organizationID, agentType, dramaID, episodeID, message, err); ok {
			return res, nil
		}
		return nil, err
	}
	raw = stripCodeFence(raw)

	var plan struct {
		Actions []struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		} `json:"actions"`
		Summary string `json:"summary"`
	}
	// If model fails to return JSON, treat whole text as summary and still try deterministic tools.
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		plan.Summary = raw
		plan.Actions = r.defaultActions(agentType, episodeID, dramaID)
	}
	if len(plan.Actions) == 0 {
		plan.Actions = r.defaultActions(agentType, episodeID, dramaID)
	}

	toolCalls := make([]map[string]any, 0)
	toolResults := make([]map[string]any, 0)
	for _, act := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		toolCalls = append(toolCalls, map[string]any{"toolName": act.Tool, "args": act.Args})
		res, err := r.execTool(agentType, organizationID, dramaID, episodeID, act.Tool, act.Args)
		if err != nil {
			toolResults = append(toolResults, map[string]any{"toolName": act.Tool, "result": "Error: " + err.Error()})
		} else {
			toolResults = append(toolResults, map[string]any{"toolName": act.Tool, "result": res})
		}
	}
	if hasToolFailure(toolResults) {
		return &ChatResult{Type: "failed", Text: "部分制作步骤失败，请查看任务详情后重试。", ToolCalls: toolCalls, ToolResults: toolResults}, nil
	}

	// Second round: if only reads happened, feed context back for write actions.
	needsWritePass := r.needsWritePass(agentType, plan.Actions)
	if maxIterations > 1 && needsWritePass {
		ctxJSON, _ := json.Marshal(toolResults)
		user2 := fmt.Sprintf(`下面是工具读取结果，请基于结果输出最终可执行 JSON（含写入工具）：
%s

要求：必须包含写入工具（save_script / save_dedup_characters / save_dedup_scenes / save_storyboards / assign_voice / generate_grid_prompt 之一）。
仅输出 JSON。`, string(ctxJSON))
		secondTemperature := temperature
		if secondTemperature > 0.3 {
			secondTemperature = 0.3
		}
		raw2, err2 := ai.ChatWithMaxTokens(ctx, cfg, system, user2, secondTemperature, maxTokens)
		if err2 == nil {
			raw2 = stripCodeFence(raw2)
			var plan2 struct {
				Actions []struct {
					Tool string         `json:"tool"`
					Args map[string]any `json:"args"`
				} `json:"actions"`
				Summary string `json:"summary"`
			}
			if json.Unmarshal([]byte(raw2), &plan2) == nil {
				for _, act := range plan2.Actions {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if r.isReadOnlyTool(act.Tool) {
						continue
					}
					toolCalls = append(toolCalls, map[string]any{"toolName": act.Tool, "args": act.Args})
					res, err := r.execTool(agentType, organizationID, dramaID, episodeID, act.Tool, act.Args)
					if err != nil {
						toolResults = append(toolResults, map[string]any{"toolName": act.Tool, "result": "Error: " + err.Error()})
					} else {
						toolResults = append(toolResults, map[string]any{"toolName": act.Tool, "result": res})
					}
				}
				if plan2.Summary != "" {
					plan.Summary = plan2.Summary
				}
			}
		}
		// deterministic fallbacks when model still fails writes
		r.ensureWrites(organizationID, agentType, dramaID, episodeID, plan.Summary, &toolCalls, &toolResults)
	} else if needsWritePass {
		// The configured model-iteration budget is exhausted. Deterministic
		// write fallbacks still complete the requested local operation.
		r.ensureWrites(organizationID, agentType, dramaID, episodeID, plan.Summary, &toolCalls, &toolResults)
	}

	if plan.Summary == "" {
		plan.Summary = "已完成 " + agentType
	}
	return &ChatResult{
		Type:        "done",
		Text:        plan.Summary,
		ToolCalls:   toolCalls,
		ToolResults: toolResults,
	}, nil
}

func hasToolFailure(results []map[string]any) bool {
	for _, result := range results {
		if value, ok := result["result"].(string); ok && strings.HasPrefix(value, "Error:") {
			return true
		}
	}
	return false
}

func (r *Runner) isReadOnlyTool(tool string) bool {
	switch tool {
	case "read_episode_script", "read_script_for_extraction", "read_existing_characters",
		"get_characters", "read_characters", "read_existing_scenes", "read_scenes",
		"read_storyboard_context", "list_voices", "read_shots_for_grid":
		return true
	default:
		return false
	}
}

func (r *Runner) needsWritePass(agentType string, actions []struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}) bool {
	hasWrite := false
	for _, a := range actions {
		if !r.isReadOnlyTool(a.Tool) {
			hasWrite = true
			break
		}
	}
	if hasWrite {
		return false
	}
	switch agentType {
	case "script_rewriter", "extractor", "storyboard_breaker", "voice_assigner", "grid_prompt_generator":
		return true
	default:
		return false
	}
}

func (r *Runner) ensureWrites(organizationID uint, agentType string, dramaID, episodeID uint, summary string, toolCalls *[]map[string]any, toolResults *[]map[string]any) {
	switch agentType {
	case "script_rewriter":
		// if summary looks like a script and no save happened, save it
		saved := false
		for _, tr := range *toolResults {
			if tr["toolName"] == "save_script" {
				saved = true
			}
		}
		if !saved && strings.Contains(summary, "##") {
			res, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_script", map[string]any{"script": summary})
			*toolCalls = append(*toolCalls, map[string]any{"toolName": "save_script", "args": map[string]any{"script": summary}})
			if err != nil {
				*toolResults = append(*toolResults, map[string]any{"toolName": "save_script", "result": err.Error()})
			} else {
				*toolResults = append(*toolResults, map[string]any{"toolName": "save_script", "result": res})
			}
		}
	case "voice_assigner":
		// auto assign from seed voices if no assign happened
		hasAssign := false
		for _, tr := range *toolResults {
			if tr["toolName"] == "assign_voice" {
				hasAssign = true
			}
		}
		if !hasAssign {
			var chars []models.Character
			db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL AND (voice_style = '' OR voice_style IS NULL)", organizationID, dramaID).Find(&chars)
			var voices []models.AIVoice
			db.DB.Where("organization_id = ?", organizationID).Find(&voices)
			if len(voices) == 0 {
				return
			}
			for i, ch := range chars {
				v := voices[i%len(voices)]
				args := map[string]any{"character_id": ch.ID, "voice_id": v.VoiceID, "voice_provider": v.Provider}
				res, err := r.execTool(agentType, organizationID, dramaID, episodeID, "assign_voice", args)
				*toolCalls = append(*toolCalls, map[string]any{"toolName": "assign_voice", "args": args})
				if err != nil {
					*toolResults = append(*toolResults, map[string]any{"toolName": "assign_voice", "result": err.Error()})
				} else {
					*toolResults = append(*toolResults, map[string]any{"toolName": "assign_voice", "result": res})
				}
			}
		}
	}
}

func (r *Runner) buildSystemPrompt(agentType string) string {
	base := defaultPrompt(agentType)
	skill := r.loadSkill(agentType)
	tools := toolCatalog(agentType)
	return base + "\n\n" + skill + "\n\n" + tools
}

func (r *Runner) resolveSystemPrompt(organizationID uint, agentType string, dramaID uint, episode models.Episode, message, legacyPrompt string) string {
	base := defaultPrompt(agentType)
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
		}
	} else if strings.TrimSpace(legacyPrompt) != "" {
		base = legacyPrompt
	}
	if version > 0 {
		base += fmt.Sprintf("\n\n提示词模板版本: %d", version)
	}
	return base + "\n\n" + r.loadSkill(agentType) + "\n\n" + toolCatalog(agentType)
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
	return "## Skill\n" + content
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

func (r *Runner) defaultActions(agentType string, episodeID, dramaID uint) []struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
} {
	// deterministic fallback pipeline steps when model JSON fails
	type act struct {
		Tool string
		Args map[string]any
	}
	switch agentType {
	case "script_rewriter":
		return []struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}{{Tool: "read_episode_script", Args: map[string]any{}}}
	case "extractor":
		return []struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}{
			{Tool: "read_script_for_extraction", Args: map[string]any{}},
			{Tool: "read_existing_characters", Args: map[string]any{}},
			{Tool: "read_existing_scenes", Args: map[string]any{}},
		}
	default:
		return nil
	}
}

func (r *Runner) execTool(agentType string, organizationID, dramaID, episodeID uint, tool string, args map[string]any) (any, error) {
	switch tool {
	case "read_episode_script", "read_script_for_extraction":
		var ep models.Episode
		if err := db.DB.Where("organization_id = ?", organizationID).First(&ep, episodeID).Error; err != nil {
			return nil, err
		}
		content := ep.ScriptContent
		if content == "" {
			content = ep.Content
		}
		return map[string]any{"content": content, "title": ep.Title}, nil

	case "save_script":
		script, _ := args["script"].(string)
		if script == "" {
			script, _ = args["content"].(string)
		}
		if script == "" {
			return nil, fmt.Errorf("script required")
		}
		if err := db.DB.Model(&models.Episode{}).Where("organization_id = ? AND id = ?", organizationID, episodeID).Updates(map[string]any{
			"script_content": script,
			"updated_at":     response.Now(),
		}).Error; err != nil {
			return nil, err
		}
		return map[string]any{"saved": true, "length": len(script)}, nil

	case "read_existing_characters", "get_characters", "read_characters":
		var chars []models.Character
		db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Find(&chars)
		return chars, nil

	case "read_existing_scenes", "read_scenes":
		var scenes []models.Scene
		db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Find(&scenes)
		return scenes, nil

	case "save_dedup_characters":
		return saveDedupCharacters(organizationID, dramaID, episodeID, args)

	case "save_dedup_scenes":
		return saveDedupScenes(organizationID, dramaID, episodeID, args)

	case "read_storyboard_context":
		var ep models.Episode
		db.DB.Where("organization_id = ?", organizationID).First(&ep, episodeID)
		var chars []models.Character
		db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Find(&chars)
		var scenes []models.Scene
		db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Find(&scenes)
		var sbs []models.Storyboard
		db.DB.Where("organization_id = ? AND episode_id = ? AND deleted_at IS NULL", organizationID, episodeID).Order("storyboard_number").Find(&sbs)
		content := ep.ScriptContent
		if content == "" {
			content = ep.Content
		}
		return map[string]any{"script": content, "characters": chars, "scenes": scenes, "existing_storyboards": sbs}, nil

	case "save_storyboards":
		return saveStoryboards(organizationID, episodeID, args)

	case "list_voices":
		var voices []models.AIVoice
		db.DB.Where("organization_id = ?", organizationID).Find(&voices)
		return voices, nil

	case "assign_voice":
		cid := asUint(args["character_id"])
		voiceID, _ := args["voice_id"].(string)
		provider, _ := args["voice_provider"].(string)
		if provider == "" {
			provider = "minimax"
		}
		if cid == 0 || voiceID == "" {
			return nil, fmt.Errorf("character_id and voice_id required")
		}
		result := db.DB.Model(&models.Character{}).Where("organization_id = ? AND drama_id = ? AND id = ? AND deleted_at IS NULL", organizationID, dramaID, cid).Updates(map[string]any{
			"voice_style":    voiceID,
			"voice_provider": provider,
			"updated_at":     response.Now(),
		})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, fmt.Errorf("character not found")
		}
		return map[string]any{"character_id": cid, "voice_id": voiceID}, nil

	case "read_shots_for_grid":
		var sbs []models.Storyboard
		db.DB.Where("organization_id = ? AND episode_id = ? AND deleted_at IS NULL", organizationID, episodeID).Order("storyboard_number").Find(&sbs)
		return sbs, nil

	case "generate_grid_prompt":
		return args, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

func saveDedupCharacters(organizationID, dramaID, episodeID uint, args map[string]any) (any, error) {
	raw, _ := json.Marshal(args["characters"])
	var items []struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		Appearance  string `json:"appearance"`
		Personality string `json:"personality"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	ts := response.Now()
	saved := 0
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			if strings.TrimSpace(it.Name) == "" {
				continue
			}
			var existing models.Character
			err := tx.Where("organization_id = ? AND drama_id = ? AND name = ? AND deleted_at IS NULL", organizationID, dramaID, it.Name).First(&existing).Error
			var cid uint
			if err == gorm.ErrRecordNotFound {
				ch := models.Character{
					OrganizationID: organizationID, DramaID: dramaID, Name: it.Name, Role: it.Role, Appearance: it.Appearance,
					Personality: it.Personality, Description: it.Description, CreatedAt: ts, UpdatedAt: ts,
				}
				if err := tx.Create(&ch).Error; err != nil {
					return err
				}
				cid = ch.ID
			} else if err != nil {
				return err
			} else {
				cid = existing.ID
				updates := map[string]any{"updated_at": ts}
				if it.Appearance != "" {
					updates["appearance"] = it.Appearance
				}
				if it.Personality != "" {
					updates["personality"] = it.Personality
				}
				if it.Description != "" {
					updates["description"] = it.Description
				}
				if it.Role != "" {
					updates["role"] = it.Role
				}
				if err := tx.Model(&existing).Updates(updates).Error; err != nil {
					return err
				}
			}
			var link models.EpisodeCharacter
			err = tx.Where("organization_id = ? AND episode_id = ? AND character_id = ?", organizationID, episodeID, cid).First(&link).Error
			if err == gorm.ErrRecordNotFound {
				if err := tx.Create(&models.EpisodeCharacter{OrganizationID: organizationID, EpisodeID: episodeID, CharacterID: cid, CreatedAt: ts}).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			saved++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": saved}, nil
}

func saveDedupScenes(organizationID, dramaID, episodeID uint, args map[string]any) (any, error) {
	raw, _ := json.Marshal(args["scenes"])
	var items []struct {
		Location string `json:"location"`
		Time     string `json:"time"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	ts := response.Now()
	saved := 0
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			if strings.TrimSpace(it.Location) == "" {
				continue
			}
			var existing models.Scene
			err := tx.Where("organization_id = ? AND drama_id = ? AND location = ? AND time = ? AND deleted_at IS NULL", organizationID, dramaID, it.Location, it.Time).First(&existing).Error
			var sid uint
			if err == gorm.ErrRecordNotFound {
				prompt := it.Prompt
				if prompt == "" {
					prompt = it.Location + ", " + it.Time
				}
				sc := models.Scene{
					OrganizationID: organizationID, DramaID: dramaID, EpisodeID: &episodeID, Location: it.Location, Time: it.Time,
					Prompt: prompt, Status: "pending", CreatedAt: ts, UpdatedAt: ts,
				}
				if err := tx.Create(&sc).Error; err != nil {
					return err
				}
				sid = sc.ID
			} else if err != nil {
				return err
			} else {
				sid = existing.ID
				if strings.TrimSpace(it.Prompt) != "" {
					if err := tx.Model(&existing).Updates(map[string]any{"prompt": it.Prompt, "updated_at": ts}).Error; err != nil {
						return err
					}
				}
			}
			var link models.EpisodeScene
			err = tx.Where("organization_id = ? AND episode_id = ? AND scene_id = ?", organizationID, episodeID, sid).First(&link).Error
			if err == gorm.ErrRecordNotFound {
				if err := tx.Create(&models.EpisodeScene{OrganizationID: organizationID, EpisodeID: episodeID, SceneID: sid, CreatedAt: ts}).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			saved++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": saved}, nil
}

func saveStoryboards(organizationID, episodeID uint, args map[string]any) (any, error) {
	raw, _ := json.Marshal(args["storyboards"])
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 || len(items) > 500 {
		return nil, fmt.Errorf("storyboards must contain between 1 and 500 items")
	}
	created := 0
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var episode models.Episode
		if err := tx.Where("organization_id = ?", organizationID).First(&episode, episodeID).Error; err != nil {
			return err
		}
		var oldIDs []uint
		if err := tx.Model(&models.Storyboard{}).Where("organization_id = ? AND episode_id = ?", organizationID, episodeID).Pluck("id", &oldIDs).Error; err != nil {
			return err
		}
		if len(oldIDs) > 0 {
			if err := tx.Where("storyboard_id IN ?", oldIDs).Delete(&models.StoryboardCharacter{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", oldIDs).Delete(&models.Storyboard{}).Error; err != nil {
				return err
			}
		}
		ts := response.Now()
		for i, item := range items {
			sceneID := asUint(item["scene_id"])
			if sceneID > 0 {
				var scene models.Scene
				if err := tx.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, sceneID, episode.DramaID).First(&scene).Error; err != nil {
					return fmt.Errorf("scene %d does not belong to episode drama", sceneID)
				}
			}
			characterIDs := uniqueAgentIDs(item["character_ids"])
			for _, characterID := range characterIDs {
				var character models.Character
				if err := tx.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, characterID, episode.DramaID).First(&character).Error; err != nil {
					return fmt.Errorf("character %d does not belong to episode drama", characterID)
				}
			}
			sb := models.Storyboard{
				OrganizationID: organizationID, EpisodeID: episodeID, StoryboardNumber: i + 1,
				Title: asString(item["title"]), Location: asString(item["location"]), Time: asString(item["time"]),
				ShotType: asString(item["shot_type"]), Angle: asString(item["angle"]), Movement: asString(item["movement"]),
				Action: asString(item["action"]), Result: asString(item["result"]), Atmosphere: asString(item["atmosphere"]),
				ImagePrompt: asString(item["image_prompt"]), VideoPrompt: asString(item["video_prompt"]),
				BGMPrompt: asString(item["bgm_prompt"]), SoundEffect: asString(item["sound_effect"]),
				Dialogue: asString(item["dialogue"]), Description: asString(item["description"]),
				Duration: asInt(item["duration"]), Status: "pending", CreatedAt: ts, UpdatedAt: ts,
			}
			if sb.Duration == 0 {
				sb.Duration = 12
			}
			if sceneID > 0 {
				sb.SceneID = &sceneID
			}
			if err := tx.Create(&sb).Error; err != nil {
				return err
			}
			for _, characterID := range characterIDs {
				if err := tx.Create(&models.StoryboardCharacter{OrganizationID: organizationID, StoryboardID: sb.ID, CharacterID: characterID}).Error; err != nil {
					return err
				}
			}
			created++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"created": created}, nil
}

func uniqueAgentIDs(value any) []uint {
	items, _ := value.([]any)
	seen := make(map[uint]struct{}, len(items))
	result := make([]uint, 0, len(items))
	for _, item := range items {
		id := asUint(item)
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func (r *Runner) offlineFallback(organizationID uint, agentType string, dramaID, episodeID uint, message string, chatErr error) (*ChatResult, bool) {
	switch agentType {
	case "script_rewriter":
		var ep models.Episode
		if err := db.DB.Where("organization_id = ?", organizationID).First(&ep, episodeID).Error; err != nil {
			return nil, false
		}
		src := ep.Content
		if src == "" {
			src = ep.ScriptContent
		}
		if src == "" {
			return nil, false
		}
		script := "## S01 | 内景 · 场景 | 日\n\n" + src + "\n"
		if _, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_script", map[string]any{"script": script}); err != nil {
			return nil, false
		}
		return &ChatResult{Type: "done", Text: "（离线回退）已将原文整理为简单格式化剧本。注意：文本模型不可用：" + chatErr.Error(), ToolCalls: []map[string]any{{"toolName": "save_script"}}, ToolResults: []map[string]any{{"toolName": "save_script", "result": "ok"}}}, true
	case "extractor":
		var ep models.Episode
		if err := db.DB.Where("organization_id = ?", organizationID).First(&ep, episodeID).Error; err != nil {
			return nil, false
		}
		text := ep.ScriptContent
		if text == "" {
			text = ep.Content
		}
		// naive name extraction: lines like 角色： or known Chinese names patterns "xxx："
		chars := []map[string]any{}
		scenes := []map[string]any{}
		seenC, seenS := map[string]bool{}, map[string]bool{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "##") {
				// ## S01 | 内景 · 咖啡厅 | 雨夜
				parts := strings.Split(line, "|")
				loc, tm := "场景", "日"
				if len(parts) >= 2 {
					loc = strings.TrimSpace(strings.ReplaceAll(parts[1], "内景", ""))
					loc = strings.TrimSpace(strings.ReplaceAll(loc, "外景", ""))
					loc = strings.Trim(loc, " ·")
				}
				if len(parts) >= 3 {
					tm = strings.TrimSpace(parts[2])
				}
				key := loc + "|" + tm
				if loc != "" && !seenS[key] {
					seenS[key] = true
					scenes = append(scenes, map[string]any{"location": loc, "time": tm, "prompt": loc + ", " + tm + ", cinematic"})
				}
			}
			if i := strings.IndexAny(line, "：:"); i > 0 && i < 12 {
				name := strings.TrimSpace(line[:i])
				name = strings.TrimSpace(regexp.MustCompile(`[（(].+?[)）]`).ReplaceAllString(name, ""))
				if name != "" && !seenC[name] && !ignoreSpeaker.MatchString(name) {
					// filter stage directions
					if !strings.Contains(name, " ") && len([]rune(name)) <= 8 {
						seenC[name] = true
						chars = append(chars, map[string]any{"name": name, "role": "角色", "description": name})
					}
				}
			}
		}
		if len(chars) == 0 {
			chars = []map[string]any{{"name": "角色A", "role": "主角", "description": "自动提取占位"}}
		}
		if len(scenes) == 0 {
			scenes = []map[string]any{{"location": "主场景", "time": "日", "prompt": "main location, daytime, cinematic"}}
		}
		_, _ = r.execTool(agentType, organizationID, dramaID, episodeID, "save_dedup_characters", map[string]any{"characters": chars})
		_, _ = r.execTool(agentType, organizationID, dramaID, episodeID, "save_dedup_scenes", map[string]any{"scenes": scenes})
		return &ChatResult{Type: "done", Text: fmt.Sprintf("（离线回退）提取角色 %d、场景 %d。文本模型不可用：%v", len(chars), len(scenes), chatErr)}, true
	case "storyboard_breaker":
		var ep models.Episode
		if err := db.DB.Where("organization_id = ?", organizationID).First(&ep, episodeID).Error; err != nil {
			return nil, false
		}
		text := ep.ScriptContent
		if text == "" {
			text = ep.Content
		}
		// split into beats by non-empty lines
		lines := []string{}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "##") {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			lines = []string{"开场建立环境", "角色互动", "情绪落点"}
		}
		// group into up to 6 shots
		shots := []map[string]any{}
		chunk := (len(lines) + 2) / 3
		if chunk < 1 {
			chunk = 1
		}
		for i := 0; i < len(lines) && len(shots) < 8; i += chunk {
			end := i + chunk
			if end > len(lines) {
				end = len(lines)
			}
			desc := strings.Join(lines[i:end], " ")
			n := len(shots) + 1
			shots = append(shots, map[string]any{
				"title":     fmt.Sprintf("镜头%d", n),
				"shot_type": "中景", "angle": "平视", "movement": "固定",
				"location": "主场景", "time": "日",
				"action": desc, "description": desc,
				"dialogue": "", "image_prompt": "cinematic shot, " + desc,
				"video_prompt": "0-4秒：" + desc,
				"duration":     12,
			})
		}
		// attach dialogue lines if any
		for _, line := range lines {
			if strings.ContainsAny(line, "：:") {
				if len(shots) > 0 {
					shots[0]["dialogue"] = line
					break
				}
			}
		}
		_, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_storyboards", map[string]any{"storyboards": shots})
		if err != nil {
			return nil, false
		}
		return &ChatResult{Type: "done", Text: fmt.Sprintf("（离线回退）已拆解 %d 个分镜。文本模型不可用：%v", len(shots), chatErr)}, true
	case "voice_assigner":
		var toolCalls []map[string]any
		var toolResults []map[string]any
		r.ensureWrites(organizationID, agentType, dramaID, episodeID, "", &toolCalls, &toolResults)
		return &ChatResult{Type: "done", Text: "（离线回退）已尝试为角色分配默认音色。", ToolCalls: toolCalls, ToolResults: toolResults}, true
	case "grid_prompt_generator":
		var sbs []models.Storyboard
		db.DB.Where("organization_id = ? AND episode_id = ? AND deleted_at IS NULL", organizationID, episodeID).Order("storyboard_number").Find(&sbs)
		var b strings.Builder
		b.WriteString("cinematic 2x2 storyboard grid, consistent style, no text\n")
		for i, sb := range sbs {
			if i >= 4 {
				break
			}
			b.WriteString(fmt.Sprintf("Panel %d: %s\n", i+1, firstNonEmptyLocal(sb.ImagePrompt, sb.Description, sb.Action)))
		}
		return &ChatResult{Type: "done", Text: b.String()}, true
	default:
		return nil, false
	}
}

func firstNonEmptyLocal(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ignoreSpeaker used by offline extractor; reuse generation-like heuristics
var ignoreSpeaker = regexp.MustCompile(`^(环境音|环境声|音效|效果音|sfx|sound ?effect|bgm|背景音|背景音乐|ambient|旁白|OS|VO)$`)

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// try extract first { ... }
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case uint:
		return int(t)
	case uint64:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case int:
		return t
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func asUint(v any) uint {
	return uint(asInt(v))
}
