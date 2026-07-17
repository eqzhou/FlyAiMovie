package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestRunnerPureHelpers(t *testing.T) {
	runner := NewRunner(t.TempDir())
	for _, agentType := range ValidAgentTypes {
		if !runner.IsValid(agentType) {
			t.Fatalf("expected %q to be valid", agentType)
		}
		if defaultPrompt(agentType) == "" || toolCatalog(agentType) == "" {
			t.Fatalf("missing prompt or tools for %q", agentType)
		}
		if !strings.Contains(runner.buildSystemPrompt(agentType), defaultPrompt(agentType)) {
			t.Fatalf("system prompt did not include default for %q", agentType)
		}
	}
	if runner.IsValid("unknown") || defaultPrompt("unknown") == "" || toolCatalog("unknown") != "" {
		t.Fatal("unexpected unknown-agent helper result")
	}

	readTools := []string{
		"read_episode_script", "read_script_for_extraction", "read_existing_characters",
		"get_characters", "read_characters", "read_existing_scenes", "read_scenes",
		"read_storyboard_context", "list_voices", "read_shots_for_grid",
	}
	for _, tool := range readTools {
		if !runner.isReadOnlyTool(tool) {
			t.Fatalf("%q should be read only", tool)
		}
	}
	if runner.isReadOnlyTool("save_script") {
		t.Fatal("save_script classified as read only")
	}

	readAction := []struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}{{Tool: "read_episode_script", Args: map[string]any{}}}
	writeAction := []struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}{{Tool: "save_script", Args: map[string]any{"script": "x"}}}
	if !runner.needsWritePass("script_rewriter", readAction) || runner.needsWritePass("script_rewriter", writeAction) || runner.needsWritePass("unknown", readAction) {
		t.Fatal("write-pass classification is incorrect")
	}

	if !hasToolFailure([]map[string]any{{"result": "Error: failed"}}) {
		t.Fatal("tool failure was not detected")
	}
	if hasToolFailure([]map[string]any{{"result": "ok"}, {"result": 1}}) {
		t.Fatal("successful tools were classified as failed")
	}

	for input, want := range map[string]string{
		"```json\n{\"actions\":[]}\n```":     "{\"actions\":[]}",
		"prefix {\"summary\":\"ok\"} suffix": "{\"summary\":\"ok\"}",
		" plain text ":                       "plain text",
	} {
		if got := stripCodeFence(input); got != want {
			t.Fatalf("stripCodeFence(%q)=%q, want %q", input, got, want)
		}
	}

	if asString(float64(1.5)) != "1.5" || asString(true) != "" {
		t.Fatal("asString conversion failed")
	}
	values := []struct {
		value any
		want  int
	}{{uint(1), 1}, {uint64(2), 2}, {int64(3), 3}, {float64(4.9), 4}, {5, 5}, {"6", 6}, {true, 0}}
	for _, tc := range values {
		if got := asInt(tc.value); got != tc.want {
			t.Fatalf("asInt(%T(%v))=%d, want %d", tc.value, tc.value, got, tc.want)
		}
	}
	if got := uniqueAgentIDs([]any{float64(2), 2, 0, float64(2), "3"}); !reflect.DeepEqual(got, []uint{2, 3}) {
		t.Fatalf("unique IDs=%v", got)
	}
	if firstNonEmptyLocal("", " ", "value") != "value" || firstNonEmptyLocal("", " ") != "" {
		t.Fatal("firstNonEmptyLocal failed")
	}
}

func TestRunnerLoadsSkillAndDefaultActions(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "extractor")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: extractor\n---\nExtract carefully."), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(dir)
	if got := runner.loadSkill("extractor"); got != "## Skill\nExtract carefully." {
		t.Fatalf("skill=%q", got)
	}
	if runner.loadSkill("missing") != "" {
		t.Fatal("missing skill should be empty")
	}
	if len(runner.defaultActions("script_rewriter", 1, 2)) != 1 || len(runner.defaultActions("extractor", 1, 2)) != 3 || runner.defaultActions("voice_assigner", 1, 2) != nil {
		t.Fatal("unexpected default actions")
	}
}

func TestRunnerExecToolContracts(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/tools.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	drama := models.Drama{OrganizationID: 9, Title: "Tool Drama", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 9, DramaID: drama.ID, EpisodeNumber: 1, Title: "Tool Episode", Content: "source", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: 9, DramaID: drama.ID, Name: "Lin", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{OrganizationID: 9, DramaID: drama.ID, Location: "Cafe", Time: "Day", Prompt: "old", CreatedAt: now, UpdatedAt: now}
	voice := models.AIVoice{OrganizationID: 9, VoiceID: "voice-1", VoiceName: "Voice", Provider: "mock", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&character, &scene, &voice} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	storyboard := models.Storyboard{OrganizationID: 9, EpisodeID: episode.ID, StoryboardNumber: 1, Title: "Shot", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir())

	for _, tool := range []string{"read_episode_script", "read_script_for_extraction", "read_existing_characters", "get_characters", "read_characters", "read_existing_scenes", "read_scenes", "read_storyboard_context", "list_voices", "read_shots_for_grid"} {
		if _, err := runner.execTool("extractor", 9, drama.ID, episode.ID, tool, nil); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	if _, err := runner.execTool("script_rewriter", 9, drama.ID, episode.ID, "save_script", map[string]any{}); err == nil {
		t.Fatal("empty script accepted")
	}
	if _, err := runner.execTool("script_rewriter", 9, drama.ID, episode.ID, "save_script", map[string]any{"content": "rewritten"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.execTool("voice_assigner", 9, drama.ID, episode.ID, "assign_voice", map[string]any{"character_id": character.ID, "voice_id": "voice-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.execTool("voice_assigner", 9, drama.ID, episode.ID, "assign_voice", map[string]any{}); err == nil {
		t.Fatal("incomplete voice assignment accepted")
	}
	if _, err := runner.execTool("voice_assigner", 9, drama.ID, episode.ID, "assign_voice", map[string]any{"character_id": 999, "voice_id": "voice-1"}); err == nil {
		t.Fatal("missing character accepted")
	}
	grid := map[string]any{"rows": 2}
	if got, err := runner.execTool("grid_prompt_generator", 9, drama.ID, episode.ID, "generate_grid_prompt", grid); err != nil || !reflect.DeepEqual(got, grid) {
		t.Fatalf("grid=%#v err=%v", got, err)
	}
	if _, err := runner.execTool("extractor", 9, drama.ID, episode.ID, "unknown", nil); err == nil {
		t.Fatal("unknown tool accepted")
	}

	characters := []map[string]any{{"name": "Lin", "role": "Lead", "appearance": "new"}, {"name": "Gu", "role": "Support"}, {"name": " "}}
	result, err := runner.execTool("extractor", 9, drama.ID, episode.ID, "save_dedup_characters", map[string]any{"characters": characters})
	if err != nil || result.(map[string]any)["saved"] != 2 {
		t.Fatalf("characters result=%#v err=%v", result, err)
	}
	scenes := []map[string]any{{"location": "Cafe", "time": "Day", "prompt": "new"}, {"location": "Street", "time": "Night", "prompt": "street"}, {"location": " "}}
	result, err = runner.execTool("extractor", 9, drama.ID, episode.ID, "save_dedup_scenes", map[string]any{"scenes": scenes})
	if err != nil || result.(map[string]any)["saved"] != 2 {
		t.Fatalf("scenes result=%#v err=%v", result, err)
	}
	if err := database.First(&character, character.ID).Error; err != nil || character.Appearance != "new" || character.VoiceProvider != "minimax" {
		t.Fatalf("character=%+v err=%v", character, err)
	}
	if err := database.First(&scene, scene.ID).Error; err != nil || scene.Prompt != "new" {
		t.Fatalf("scene=%+v err=%v", scene, err)
	}
}
