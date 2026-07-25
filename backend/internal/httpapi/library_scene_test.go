package httpapi

import (
	"net/http"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestCharacterTemplateImportCreatesIndependentCharacter(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "library", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	created := performRequest(router, http.MethodPost, "/api/v1/character-library", `{"name":"Ada","appearance":"red coat"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	templateID := uint(decodeResponse(t, created)["data"].(map[string]any)["id"].(float64))
	imported := performRequest(router, http.MethodPost, "/api/v1/character-library/"+itoa(templateID)+"/import", `{"drama_id":`+itoa(drama.ID)+`,"episode_id":`+itoa(episode.ID)+`}`, nil)
	if imported.Code != http.StatusCreated {
		t.Fatalf("import=%d %s", imported.Code, imported.Body.String())
	}
	if err := db.DB.Model(&models.CharacterTemplate{}).Where("id = ?", templateID).Update("appearance", "blue coat").Error; err != nil {
		t.Fatal(err)
	}
	var character models.Character
	if err := db.DB.Where("drama_id = ?", drama.ID).First(&character).Error; err != nil {
		t.Fatal(err)
	}
	if character.Appearance != "red coat" {
		t.Fatalf("character mutated with template: %q", character.Appearance)
	}
}

func TestSceneMoveRequiresExplicitCrossDramaAndMovesStoryboards(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	dramaA := models.Drama{Title: "A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "B", CreatedAt: now, UpdatedAt: now}
	db.DB.Create(&dramaA)
	db.DB.Create(&dramaB)
	episodeA := models.Episode{DramaID: dramaA.ID, EpisodeNumber: 1, Title: "A1", CreatedAt: now, UpdatedAt: now}
	episodeB := models.Episode{DramaID: dramaB.ID, EpisodeNumber: 1, Title: "B1", CreatedAt: now, UpdatedAt: now}
	db.DB.Create(&episodeA)
	db.DB.Create(&episodeB)
	scene := models.Scene{DramaID: dramaA.ID, EpisodeID: &episodeA.ID, Location: "room", Time: "day", Prompt: "room", CreatedAt: now, UpdatedAt: now}
	db.DB.Create(&scene)
	shot := models.Storyboard{EpisodeID: episodeA.ID, SceneID: &scene.ID, StoryboardNumber: 1, CreatedAt: now, UpdatedAt: now}
	db.DB.Create(&shot)
	rejected := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(scene.ID)+"/move", `{"episode_id":`+itoa(episodeB.ID)+`}`, nil)
	if rejected.Code != http.StatusConflict {
		t.Fatalf("rejected=%d %s", rejected.Code, rejected.Body.String())
	}
	moved := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(scene.ID)+"/move", `{"episode_id":`+itoa(episodeB.ID)+`,"allow_cross_drama":true,"move_storyboards":true}`, nil)
	if moved.Code != http.StatusOK {
		t.Fatalf("move=%d %s", moved.Code, moved.Body.String())
	}
	db.DB.First(&shot, shot.ID)
	if shot.EpisodeID != episodeB.ID {
		t.Fatalf("shot episode=%d", shot.EpisodeID)
	}
}

func TestCharacterSaveToLibraryCreatesTemplateCopy(t *testing.T) {
	_, router := testServerRouter(t)
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"save library","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"name":"林舟","role":"男主","appearance":"black coat","personality":"calm","description":"lead"}`)
	characterID := uintField(t, character, "id")

	saved := requestData(t, router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/save-to-library", `{}`)
	templateID := uintField(t, saved, "id")
	if saved["name"] != "林舟" || saved["appearance"] != "black coat" {
		t.Fatalf("template fields=%+v", saved)
	}

	// mutating original character must not change saved template
	if resp := performRequest(router, http.MethodPut, "/api/v1/characters/"+itoa(characterID), `{"appearance":"white coat"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("update character status=%d body=%s", resp.Code, resp.Body.String())
	}
	var template models.CharacterTemplate
	if err := db.DB.First(&template, templateID).Error; err != nil {
		t.Fatal(err)
	}
	if template.Appearance != "black coat" {
		t.Fatalf("template mutated with character: %q", template.Appearance)
	}

	// deleted character cannot be saved
	if resp := performRequest(router, http.MethodDelete, "/api/v1/characters/"+itoa(characterID), "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete character status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/save-to-library", `{}`, nil); resp.Code != http.StatusNotFound && resp.Code != http.StatusBadRequest {
		t.Fatalf("save deleted character status=%d body=%s", resp.Code, resp.Body.String())
	}
}
