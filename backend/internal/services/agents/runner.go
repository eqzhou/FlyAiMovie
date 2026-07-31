package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/prompttemplate"
	"github.com/eqzhou/flyaimovie/internal/services/skillregistry"
	"gorm.io/gorm"
)

var ValidAgentTypes = []string{
	"script_rewriter",
	"extractor",
	"storyboard_breaker",
	"voice_assigner",
	"grid_prompt_generator",
}

const (
	maxAgentActions   = 32
	maxAgentArgsBytes = 64 * 1024
)

type AgentAction struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type AgentPlan struct {
	Actions []AgentAction `json:"actions"`
	Summary string        `json:"summary"`
}

type ChatResult struct {
	Type        string           `json:"type"`
	Text        string           `json:"text"`
	ToolCalls   []map[string]any `json:"toolCalls"`
	ToolResults []map[string]any `json:"toolResults"`
}

type RunEvent struct {
	EventType string
	ToolName  string
	Payload   map[string]any
}

type EventObserver func(RunEvent)

type PromptResolution struct {
	System                   string
	Source                   string
	TemplateID               uint
	Key                      string
	Version                  int
	SkillSource              string
	SkillID                  uint
	SkillVersionID           uint
	SkillVersion             int
	SkillHash                string
	SkillSnapshot            string
	SkillSnapshotMode        string
	SkillSnapshotSourceRunID uint
}

// SkillSnapshotOverride freezes the exact Skill content and provenance used by
// an earlier run. It deliberately does not freeze the model, AgentConfig, or
// organization prompt template.
type SkillSnapshotOverride struct {
	SourceRunID    uint
	SkillSource    string
	SkillID        uint
	SkillVersionID uint
	SkillVersion   int
	SkillHash      string
	SkillSnapshot  string
}

type RunOptions struct {
	SkillSnapshot *SkillSnapshotOverride
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
	return r.RunObserved(ctx, organizationID, agentType, dramaID, episodeID, message, nil)
}

func (r *Runner) RunObserved(ctx context.Context, organizationID uint, agentType string, dramaID, episodeID uint, message string, observer EventObserver) (*ChatResult, error) {
	return r.RunObservedWithOptions(ctx, organizationID, agentType, dramaID, episodeID, message, observer, RunOptions{})
}

func (r *Runner) RunObservedWithOptions(ctx context.Context, organizationID uint, agentType string, dramaID, episodeID uint, message string, observer EventObserver, options RunOptions) (*ChatResult, error) {
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
		if organizationID == 0 {
			cfg, err = ai.GetActiveConfig("text", &textConfig.ID)
		} else {
			cfg, err = ai.GetOrganizationConfig(organizationID, "text", &textConfig.ID)
		}
	} else {
		err = configErr
	}
	if err != nil {
		return nil, err
	}
	var agentConfig models.AgentConfig
	if configErr := db.DB.Where("organization_id = ? AND agent_type = ? AND is_active = ? AND deleted_at IS NULL", organizationID, agentType, true).Order("id desc").First(&agentConfig).Error; configErr == nil {
		if agentConfig.Model != "" {
			cfg.Model = agentConfig.Model
		}
	}

	resolution, err := r.resolveSystemPromptWithOptions(organizationID, agentType, dramaID, requestedEpisode, message, agentConfig.SystemPrompt, options)
	if err != nil {
		return nil, err
	}
	system := resolution.System
	emitRunEvent(observer, RunEvent{EventType: "prompt_resolved", Payload: map[string]any{
		"source": resolution.Source, "template_id": resolution.TemplateID, "key": resolution.Key, "version": resolution.Version,
		"skill_source": resolution.SkillSource, "skill_id": resolution.SkillID, "skill_version_id": resolution.SkillVersionID,
		"skill_version": resolution.SkillVersion, "skill_hash": resolution.SkillHash, "skill_snapshot": resolution.SkillSnapshot,
		"skill_snapshot_mode": resolution.SkillSnapshotMode, "skill_snapshot_source_run_id": resolution.SkillSnapshotSourceRunID,
	}})
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
	user := fmt.Sprintf(`项目ID=%d 剧集ID=%d
用户请求：%s

你必须只输出 JSON（不要 markdown，不要自然语言寒暄）：
{
  "actions":[{"tool":"工具名","args":{...}}],
  "summary":"给用户的中文总结"
}

规则：
1. 第一轮若信息不足，可先调用只读工具。
2. 若已有足够上下文，actions 必须直接包含写入工具（save_script / save_dedup_characters / save_dedup_scenes / save_storyboards / assign_voice / generate_grid_prompt）。
3. summary 必须是完成后的结果说明，不要写“正在读取/准备中”。
4. 可用工具取决于 agent 类型。`, dramaID, episodeID, message)

	raw, err := ai.ChatWithMaxTokens(ctx, cfg, system, user, temperature, maxTokens)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cfg.Provider == "mock" {
			if res, ok := r.offlineFallback(organizationID, agentType, dramaID, episodeID, message, err); ok {
				emitResultEvents(observer, res)
				return res, nil
			}
		}
		return nil, fmt.Errorf("text provider request failed")
	}
	raw = stripCodeFence(raw)

	var plan AgentPlan
	// If model fails to return JSON, treat whole text as summary and still try deterministic tools.
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		plan.Summary = raw
		plan.Actions = r.defaultActions(agentType, episodeID, dramaID)
	}
	if len(plan.Actions) == 0 {
		plan.Actions = r.defaultActions(agentType, episodeID, dramaID)
	}
	if err := validateAgentActions(agentType, plan.Actions, 0); err != nil {
		return invalidAgentPlanResult(err, nil, nil), nil
	}

	toolCalls := make([]map[string]any, 0)
	toolResults := make([]map[string]any, 0)
	for _, act := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		call, result := r.executeObservedTool(observer, agentType, organizationID, dramaID, episodeID, act.Tool, act.Args)
		toolCalls = append(toolCalls, call)
		toolResults = append(toolResults, result)
	}
	if hasToolFailure(toolResults) {
		return &ChatResult{Type: "failed", Text: "部分制作步骤失败，请查看任务详情后重试。", ToolCalls: toolCalls, ToolResults: toolResults}, nil
	}

	// Second round: if only reads happened, feed context back for write actions.
	needsWritePass := r.needsWritePass(agentType, plan.Actions)
	if maxIterations > 1 && needsWritePass {
		ctxJSON, _ := json.Marshal(toolResults)
		user2 := fmt.Sprintf(`下面是工具读取结果，请基于结果输出最终可执行 JSON（必须含写入工具）：
%s

要求：
1. 必须包含写入工具（save_script / save_dedup_characters / save_dedup_scenes / save_storyboards / assign_voice / generate_grid_prompt 之一）。
2. 不要只返回读取工具。
3. summary 写完成后的结果，不要写“正在…”。
仅输出 JSON。`, string(ctxJSON))
		secondTemperature := temperature
		if secondTemperature > 0.3 {
			secondTemperature = 0.3
		}
		raw2, err2 := ai.ChatWithMaxTokens(ctx, cfg, system, user2, secondTemperature, maxTokens)
		if err2 != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err2 == nil {
			raw2 = stripCodeFence(raw2)
			var plan2 AgentPlan
			if json.Unmarshal([]byte(raw2), &plan2) == nil {
				if err := validateAgentActions(agentType, plan2.Actions, len(toolCalls)); err != nil {
					return invalidAgentPlanResult(err, toolCalls, toolResults), nil
				}
				for _, act := range plan2.Actions {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if r.isReadOnlyTool(act.Tool) {
						continue
					}
					call, result := r.executeObservedTool(observer, agentType, organizationID, dramaID, episodeID, act.Tool, act.Args)
					toolCalls = append(toolCalls, call)
					toolResults = append(toolResults, result)
				}
				if plan2.Summary != "" {
					plan.Summary = plan2.Summary
				}
			}
		}
		// deterministic fallbacks when model still fails writes
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.ensureWrites(observer, organizationID, agentType, dramaID, episodeID, plan.Summary, &toolCalls, &toolResults)
	} else if needsWritePass {
		// The configured model-iteration budget is exhausted. Deterministic
		// write fallbacks still complete the requested local operation.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.ensureWrites(observer, organizationID, agentType, dramaID, episodeID, plan.Summary, &toolCalls, &toolResults)
	}
	if hasToolFailure(toolResults) {
		return &ChatResult{Type: "failed", Text: "部分制作步骤失败，请查看任务详情后重试。", ToolCalls: toolCalls, ToolResults: toolResults}, nil
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

func invalidAgentPlanResult(err error, toolCalls, toolResults []map[string]any) *ChatResult {
	return &ChatResult{
		Type:        "failed",
		Text:        "Agent 输出校验失败：" + err.Error(),
		ToolCalls:   toolCalls,
		ToolResults: toolResults,
	}
}

func validateAgentActions(agentType string, actions []AgentAction, already int) error {
	if already < 0 || len(actions) > maxAgentActions-already {
		return fmt.Errorf("动作数量超过限制（累计最多 %d 个）", maxAgentActions)
	}
	for index, action := range actions {
		if !allowedAgentTool(agentType, action.Tool) {
			return fmt.Errorf("第 %d 个动作的工具 %q 不属于 Agent %q", index+1, action.Tool, agentType)
		}
		args := action.Args
		if args == nil {
			args = map[string]any{}
		}
		encoded, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("第 %d 个动作参数无法编码: %w", index+1, err)
		}
		if len(encoded) > maxAgentArgsBytes {
			return fmt.Errorf("第 %d 个动作参数过大（最大 %d KiB）", index+1, maxAgentArgsBytes/1024)
		}
	}
	return nil
}

func allowedAgentTool(agentType, tool string) bool {
	switch agentType {
	case "script_rewriter":
		return tool == "read_episode_script" || tool == "save_script"
	case "extractor":
		switch tool {
		case "read_script_for_extraction", "read_existing_characters", "read_existing_scenes",
			"save_dedup_characters", "save_dedup_scenes":
			return true
		}
	case "storyboard_breaker":
		return tool == "read_storyboard_context" || tool == "save_storyboards"
	case "voice_assigner":
		return tool == "list_voices" || tool == "get_characters" || tool == "assign_voice"
	case "grid_prompt_generator":
		switch tool {
		case "read_characters", "read_scenes", "read_shots_for_grid", "generate_grid_prompt":
			return true
		}
	}
	return false
}

func emitRunEvent(observer EventObserver, event RunEvent) {
	if observer != nil {
		observer(event)
	}
}

func emitResultEvents(observer EventObserver, result *ChatResult) {
	if observer == nil || result == nil {
		return
	}
	for index, call := range result.ToolCalls {
		toolName, _ := call["toolName"].(string)
		emitRunEvent(observer, RunEvent{EventType: "tool_call", ToolName: toolName, Payload: call})
		if index < len(result.ToolResults) {
			emitRunEvent(observer, RunEvent{EventType: "tool_result", ToolName: toolName, Payload: result.ToolResults[index]})
		}
	}
}

func (r *Runner) executeObservedTool(observer EventObserver, agentType string, organizationID, dramaID, episodeID uint, toolName string, args map[string]any) (map[string]any, map[string]any) {
	call := map[string]any{"toolName": toolName, "args": args}
	emitRunEvent(observer, RunEvent{EventType: "tool_call", ToolName: toolName, Payload: call})
	value, err := r.execTool(agentType, organizationID, dramaID, episodeID, toolName, args)
	result := map[string]any{"toolName": toolName, "result": value}
	if err != nil {
		result["result"] = "Error: " + err.Error()
	}
	emitRunEvent(observer, RunEvent{EventType: "tool_result", ToolName: toolName, Payload: result})
	return call, result
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

func (r *Runner) needsWritePass(agentType string, actions []AgentAction) bool {
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

func finalizeAgentSummary(agentType, summary string, toolResults []map[string]any) string {
	summary = strings.TrimSpace(summary)
	hasWrite := false
	for _, tr := range toolResults {
		name, _ := tr["toolName"].(string)
		switch name {
		case "save_script", "save_dedup_characters", "save_dedup_scenes", "save_storyboards", "assign_voice", "generate_grid_prompt":
			hasWrite = true
		}
	}
	if !hasWrite {
		return summary
	}
	planning := summary == "" || strings.Contains(summary, "正在") || strings.Contains(summary, "准备") || strings.Contains(summary, "已请求读取")
	if !planning {
		return summary
	}
	switch agentType {
	case "script_rewriter":
		return "已完成剧本改写并保存。"
	case "extractor":
		return "已完成角色与场景提取并保存。"
	case "storyboard_breaker":
		return "已完成分镜拆解并保存。"
	case "voice_assigner":
		return "已完成音色分配。"
	case "grid_prompt_generator":
		return "已生成宫格提示词。"
	default:
		return "已完成写入。"
	}
}

func (r *Runner) ensureWrites(observer EventObserver, organizationID uint, agentType string, dramaID, episodeID uint, summary string, toolCalls *[]map[string]any, toolResults *[]map[string]any) {
	hasTool := func(name string) bool {
		for _, tr := range *toolResults {
			if tr["toolName"] == name {
				return true
			}
		}
		return false
	}
	runTool := func(name string, args map[string]any) {
		call, result := r.executeObservedTool(observer, agentType, organizationID, dramaID, episodeID, name, args)
		*toolCalls = append(*toolCalls, call)
		*toolResults = append(*toolResults, result)
	}

	switch agentType {
	case "script_rewriter":
		if hasTool("save_script") {
			return
		}
		script := strings.TrimSpace(summary)
		if !strings.Contains(script, "##") {
			// Prefer source content over a planning sentence so write pass still lands.
			var episode models.Episode
			if err := db.DB.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, episodeID, dramaID).First(&episode).Error; err == nil {
				source := strings.TrimSpace(episode.Content)
				if source == "" {
					source = strings.TrimSpace(episode.ScriptContent)
				}
				if source != "" {
					script = formatOfflineScript(source)
				}
			}
		}
		if script == "" {
			return
		}
		runTool("save_script", map[string]any{"script": script})

	case "extractor":
		needChars := !hasTool("save_dedup_characters")
		needScenes := !hasTool("save_dedup_scenes")
		if !needChars && !needScenes {
			return
		}
		var episode models.Episode
		if err := db.DB.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, episodeID, dramaID).First(&episode).Error; err != nil {
			return
		}
		source := strings.TrimSpace(episode.ScriptContent)
		if source == "" {
			source = strings.TrimSpace(episode.Content)
		}
		if source == "" {
			return
		}
		if needChars {
			if chars := extractOfflineCharacters(source); len(chars) > 0 {
				runTool("save_dedup_characters", map[string]any{"characters": chars})
			}
		}
		if needScenes {
			if scenes := extractOfflineScenes(source); len(scenes) > 0 {
				runTool("save_dedup_scenes", map[string]any{"scenes": scenes})
			}
		}

	case "storyboard_breaker":
		if hasTool("save_storyboards") {
			return
		}
		var episode models.Episode
		if err := db.DB.Where("organization_id = ? AND id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, episodeID, dramaID).First(&episode).Error; err != nil {
			return
		}
		source := strings.TrimSpace(episode.ScriptContent)
		if source == "" {
			source = strings.TrimSpace(episode.Content)
		}
		if source == "" {
			return
		}
		var scenes []models.Scene
		_ = db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&scenes).Error
		if shots := extractOfflineStoryboards(source, scenes); len(shots) > 0 {
			runTool("save_storyboards", map[string]any{"storyboards": shots})
		}

	case "grid_prompt_generator":
		if hasTool("generate_grid_prompt") {
			return
		}
		runTool("generate_grid_prompt", map[string]any{"episode_id": episodeID, "rows": 2, "cols": 2, "mode": "first_frame"})

	case "voice_assigner":
		if hasTool("assign_voice") {
			return
		}
		var chars []models.Character
		db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL AND (voice_style = '' OR voice_style IS NULL)", organizationID, dramaID).Find(&chars)
		var voices []models.AIVoice
		db.DB.Where("organization_id = ?", organizationID).Find(&voices)
		if len(voices) == 0 || len(chars) == 0 {
			return
		}
		for i, ch := range chars {
			v := voices[i%len(voices)]
			runTool("assign_voice", map[string]any{"character_id": ch.ID, "voice_id": v.VoiceID, "voice_provider": v.Provider})
		}
	}
}

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

func formatOfflineScript(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if strings.Contains(source, "##") {
		return source
	}
	return "## S01 | 内景 · 场景 | 日\n\n" + source + "\n"
}

func extractOfflineCharacters(text string) []map[string]any {
	chars := []map[string]any{}
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexAny(line, "：:"); i > 0 && i < 12 {
			name := strings.TrimSpace(line[:i])
			name = strings.TrimSpace(regexp.MustCompile(`[（(].+?[)）]`).ReplaceAllString(name, ""))
			if name == "" || seen[name] || ignoreSpeaker.MatchString(name) {
				continue
			}
			if !strings.Contains(name, " ") && len([]rune(name)) <= 8 {
				seen[name] = true
				chars = append(chars, map[string]any{"name": name, "role": "角色", "description": name, "appearance": name})
			}
		}
	}
	if len(chars) == 0 {
		chars = []map[string]any{{"name": "角色A", "role": "主角", "description": "自动提取占位", "appearance": "待补充"}}
	}
	return chars
}

func extractOfflineScenes(text string) []map[string]any {
	scenes := []map[string]any{}
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "##") {
			continue
		}
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
		if loc != "" && !seen[key] {
			seen[key] = true
			scenes = append(scenes, map[string]any{"location": loc, "time": tm, "prompt": loc + ", " + tm + ", cinematic"})
		}
	}
	if len(scenes) == 0 {
		// free-form: first line often has location cue
		loc := "主场景"
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// naive: take first short clause
			loc = line
			if len([]rune(loc)) > 16 {
				loc = string([]rune(loc)[:16])
			}
			break
		}
		scenes = []map[string]any{{"location": loc, "time": "日", "prompt": loc + ", daytime, cinematic"}}
	}
	return scenes
}

func extractOfflineStoryboards(text string, scenes []models.Scene) []map[string]any {
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
	shots := []map[string]any{}
	chunk := (len(lines) + 2) / 3
	if chunk < 1 {
		chunk = 1
	}
	sceneID := uint(0)
	location, timeOfDay := "主场景", "日"
	if len(scenes) > 0 {
		sceneID = scenes[0].ID
		if scenes[0].Location != "" {
			location = scenes[0].Location
		}
		if scenes[0].Time != "" {
			timeOfDay = scenes[0].Time
		}
	}
	for i := 0; i < len(lines) && len(shots) < 8; i += chunk {
		end := i + chunk
		if end > len(lines) {
			end = len(lines)
		}
		desc := strings.Join(lines[i:end], " ")
		n := len(shots) + 1
		shot := map[string]any{
			"title": fmt.Sprintf("镜头%d", n), "shot_type": "中景", "angle": "平视", "movement": "固定",
			"location": location, "time": timeOfDay, "action": desc, "description": desc,
			"dialogue": "", "image_prompt": "cinematic shot, " + desc, "video_prompt": "0-4秒：" + desc, "duration": 12,
		}
		if sceneID > 0 {
			shot["scene_id"] = sceneID
		}
		shots = append(shots, shot)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "：:") {
			if len(shots) > 0 {
				shots[0]["dialogue"] = line
			}
			break
		}
	}
	return shots
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
		script := formatOfflineScript(src)
		if script == "" {
			return nil, false
		}
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
		chars := extractOfflineCharacters(text)
		scenes := extractOfflineScenes(text)
		if _, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_dedup_characters", map[string]any{"characters": chars}); err != nil {
			return &ChatResult{Type: "failed", Text: "离线角色写入失败，请重试。", ToolResults: []map[string]any{{"toolName": "save_dedup_characters", "result": "Error: " + err.Error()}}}, true
		}
		if _, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_dedup_scenes", map[string]any{"scenes": scenes}); err != nil {
			return &ChatResult{Type: "failed", Text: "离线场景写入失败，请重试。", ToolResults: []map[string]any{{"toolName": "save_dedup_scenes", "result": "Error: " + err.Error()}}}, true
		}
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
		var scenes []models.Scene
		_ = db.DB.Where("organization_id = ? AND drama_id = ? AND deleted_at IS NULL", organizationID, dramaID).Order("id").Find(&scenes).Error
		shots := extractOfflineStoryboards(text, scenes)
		if _, err := r.execTool(agentType, organizationID, dramaID, episodeID, "save_storyboards", map[string]any{"storyboards": shots}); err != nil {
			return nil, false
		}
		return &ChatResult{Type: "done", Text: fmt.Sprintf("（离线回退）已拆解 %d 个分镜。文本模型不可用：%v", len(shots), chatErr)}, true
	case "voice_assigner":
		var toolCalls []map[string]any
		var toolResults []map[string]any
		r.ensureWrites(nil, organizationID, agentType, dramaID, episodeID, "", &toolCalls, &toolResults)
		if hasToolFailure(toolResults) {
			return &ChatResult{Type: "failed", Text: "离线音色分配失败，请重试。", ToolCalls: toolCalls, ToolResults: toolResults}, true
		}
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
