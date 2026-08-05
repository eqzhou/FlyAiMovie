package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
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
