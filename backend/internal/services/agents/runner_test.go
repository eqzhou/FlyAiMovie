package agents

import (
	"context"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestSaveStoryboardsIsTransactionalAndEnforcesOwnership(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/agents.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&dramaA).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&dramaB).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	localScene := models.Scene{DramaID: dramaA.ID, Location: "local", Time: "day", Prompt: "local", CreatedAt: now, UpdatedAt: now}
	foreignScene := models.Scene{DramaID: dramaB.ID, Location: "foreign", Time: "day", Prompt: "foreign", CreatedAt: now, UpdatedAt: now}
	localCharacter := models.Character{DramaID: dramaA.ID, Name: "local", CreatedAt: now, UpdatedAt: now}
	foreignCharacter := models.Character{DramaID: dramaB.ID, Name: "foreign", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&localScene, &foreignScene, &localCharacter, &foreignCharacter} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	original := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "original", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	originalLink := models.StoryboardCharacter{StoryboardID: original.ID, CharacterID: localCharacter.ID}
	if err := database.Create(&originalLink).Error; err != nil {
		t.Fatal(err)
	}

	_, err = saveStoryboards(0, episode.ID, map[string]any{"storyboards": []map[string]any{{
		"title": "invalid", "scene_id": foreignScene.ID, "character_ids": []any{float64(foreignCharacter.ID)},
	}}})
	if err == nil {
		t.Fatal("cross-drama resources were accepted")
	}
	var stillOriginal models.Storyboard
	if err := database.First(&stillOriginal, original.ID).Error; err != nil || stillOriginal.Title != "original" {
		t.Fatalf("failed replacement did not roll back: %+v err=%v", stillOriginal, err)
	}
	var stillLinked models.StoryboardCharacter
	if err := database.Where("storyboard_id = ? AND character_id = ?", original.ID, localCharacter.ID).First(&stillLinked).Error; err != nil {
		t.Fatalf("original character link was lost: %v", err)
	}

	result, err := saveStoryboards(0, episode.ID, map[string]any{"storyboards": []map[string]any{{
		"title": "replacement", "scene_id": localScene.ID,
		"character_ids": []any{float64(localCharacter.ID), float64(localCharacter.ID)},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["created"] != 1 {
		t.Fatalf("result=%#v", result)
	}
	var oldLinkCount int64
	database.Model(&models.StoryboardCharacter{}).Where("storyboard_id = ?", original.ID).Count(&oldLinkCount)
	if oldLinkCount != 0 {
		t.Fatalf("old links remain: %d", oldLinkCount)
	}
	var replacement models.Storyboard
	if err := database.Where("episode_id = ?", episode.ID).First(&replacement).Error; err != nil {
		t.Fatal(err)
	}
	var replacementLinks int64
	database.Model(&models.StoryboardCharacter{}).Where("storyboard_id = ?", replacement.ID).Count(&replacementLinks)
	if replacementLinks != 1 {
		t.Fatalf("deduplicated links=%d, want 1", replacementLinks)
	}
}

func TestOfflineFallbackAgentsPersistWorkflow(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/offline-agents.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	drama := models.Drama{OrganizationID: 7, Title: "独立短剧", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 7, DramaID: drama.ID, EpisodeNumber: 1, Title: "第一集", Content: "## S01 | 内景 · 咖啡厅 | 雨夜\n林岚：我们必须马上离开。\n顾川：走后门。", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	voice := models.AIVoice{OrganizationID: 7, VoiceID: "voice-test", VoiceName: "测试音色", Provider: "mock", CreatedAt: now}
	if err := database.Create(&voice).Error; err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(t.TempDir())
	var voiceResult *ChatResult

	for _, agentType := range []string{"script_rewriter", "extractor", "storyboard_breaker", "voice_assigner", "grid_prompt_generator"} {
		result, err := runner.Run(context.Background(), 7, agentType, drama.ID, episode.ID, "执行当前制作步骤")
		if err != nil {
			t.Fatalf("%s: %v", agentType, err)
		}
		if result.Type != "done" || result.Text == "" {
			t.Fatalf("%s result=%+v", agentType, result)
		}
		if agentType == "grid_prompt_generator" && !strings.Contains(result.Text, "Panel 1") {
			t.Fatalf("grid prompt=%q", result.Text)
		}
		if agentType == "voice_assigner" {
			voiceResult = result
		}
	}

	if err := database.First(&episode, episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(episode.ScriptContent, "## S01") {
		t.Fatalf("script was not saved: %q", episode.ScriptContent)
	}
	var characters []models.Character
	if err := database.Where("organization_id = ? AND drama_id = ?", 7, drama.ID).Find(&characters).Error; err != nil {
		t.Fatal(err)
	}
	if len(characters) < 2 {
		t.Fatalf("characters=%+v", characters)
	}
	for _, character := range characters {
		if character.VoiceStyle != voice.VoiceID {
			t.Fatalf("voice not assigned: %+v result=%+v", character, voiceResult)
		}
	}
	var scenes []models.Scene
	if err := database.Where("organization_id = ? AND drama_id = ?", 7, drama.ID).Find(&scenes).Error; err != nil {
		t.Fatal(err)
	}
	if len(scenes) == 0 {
		t.Fatal("scene extraction persisted no scenes")
	}
	var storyboards []models.Storyboard
	if err := database.Where("organization_id = ? AND episode_id = ?", 7, episode.ID).Find(&storyboards).Error; err != nil {
		t.Fatal(err)
	}
	if len(storyboards) == 0 || storyboards[0].ImagePrompt == "" || storyboards[0].VideoPrompt == "" {
		t.Fatalf("storyboards=%+v", storyboards)
	}
	var characterLinks, sceneLinks int64
	database.Model(&models.EpisodeCharacter{}).Where("organization_id = ? AND episode_id = ?", 7, episode.ID).Count(&characterLinks)
	database.Model(&models.EpisodeScene{}).Where("organization_id = ? AND episode_id = ?", 7, episode.ID).Count(&sceneLinks)
	if characterLinks != int64(len(characters)) || sceneLinks == 0 {
		t.Fatalf("links characters=%d scenes=%d", characterLinks, sceneLinks)
	}
}
