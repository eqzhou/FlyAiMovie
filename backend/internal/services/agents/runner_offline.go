package agents

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/textutil"
)

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
			b.WriteString(fmt.Sprintf("Panel %d: %s\n", i+1, textutil.FirstNonBlank(sb.ImagePrompt, sb.Description, sb.Action)))
		}
		return &ChatResult{Type: "done", Text: b.String()}, true
	default:
		return nil, false
	}
}
