package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestMockAssetGenerationWorkflow(t *testing.T) {
	server, router := testServerRouter(t)
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"asset generation","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")

	character := requestData(t, router, http.MethodPost, "/api/v1/characters", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"林舟","role":"主角","appearance":"黑发，深色外套","voice_style":"mock-voice"}`)
	characterID := uintField(t, character, "id")
	scene := requestData(t, router, http.MethodPost, "/api/v1/scenes", `{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"location":"旧车站","time":"夜","prompt":"old station at night"}`)
	sceneID := uintField(t, scene, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props", `{"drama_id":`+itoa(dramaID)+`,"name":"怀表","type":"prop","prompt":"antique pocket watch"}`)
	propID := uintField(t, prop, "id")

	charImage := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if charImage.Code != http.StatusOK {
		t.Fatalf("character image status=%d body=%s", charImage.Code, charImage.Body.String())
	}
	sceneImage := performRequest(router, http.MethodPost, "/api/v1/scenes/"+itoa(sceneID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if sceneImage.Code != http.StatusOK {
		t.Fatalf("scene image status=%d body=%s", sceneImage.Code, sceneImage.Body.String())
	}
	propImage := performRequest(router, http.MethodPost, "/api/v1/props/"+itoa(propID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if propImage.Code != http.StatusOK {
		t.Fatalf("prop image status=%d body=%s", propImage.Code, propImage.Body.String())
	}

	var characterRow models.Character
	var sceneRow models.Scene
	var propRow models.Prop
	if err := db.DB.First(&characterRow, characterID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.First(&sceneRow, sceneID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.First(&propRow, propID).Error; err != nil {
		t.Fatal(err)
	}
	if characterRow.ImageURL == "" || sceneRow.ImageURL == "" || propRow.ImageURL == "" {
		t.Fatalf("generated asset URLs missing: character=%q scene=%q prop=%q", characterRow.ImageURL, sceneRow.ImageURL, propRow.ImageURL)
	}
	assets := performRequest(router, http.MethodGet, "/api/v1/assets?drama_id="+itoa(dramaID), "", nil)
	if assets.Code != http.StatusOK || !strings.Contains(assets.Body.String(), "character") || !strings.Contains(assets.Body.String(), "scene") || !strings.Contains(assets.Body.String(), "prop") {
		t.Fatalf("asset library missing generated categories: status=%d body=%s", assets.Code, assets.Body.String())
	}

	voice := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-voice-sample", `{"episode_id":`+itoa(episodeID)+`}`, nil)
	if voice.Code != http.StatusOK || !strings.Contains(voice.Body.String(), "voice_sample_url") {
		t.Fatalf("voice sample status=%d body=%s", voice.Code, voice.Body.String())
	}
	_ = server
}

// Character and prop reference images must reach the image generation record,
// otherwise saved references are silently ignored and appearance drifts
// between generations.
func TestCharacterAndPropImageGenerationForwardsReferenceImages(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"reference forwarding","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")

	now := response.Now()
	owned := models.ImageGeneration{ImageURL: "/static/character-reference.png", LocalPath: "character-reference.png", Status: "completed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	references := `["/static/character-reference.png"]`

	character := requestData(t, router, http.MethodPost, "/api/v1/characters",
		`{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"参考角色","appearance":"红色风衣","reference_images":`+strconv.Quote(references)+`}`)
	characterID := uintField(t, character, "id")
	prop := requestData(t, router, http.MethodPost, "/api/v1/props",
		`{"drama_id":`+itoa(dramaID)+`,"name":"参考道具","prompt":"antique lantern","reference_images":`+strconv.Quote(references)+`}`)
	propID := uintField(t, prop, "id")

	var storedCharacter models.Character
	if err := db.DB.First(&storedCharacter, characterID).Error; err != nil {
		t.Fatal(err)
	}
	var storedProp models.Prop
	if err := db.DB.First(&storedProp, propID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCharacter.ReferenceImages != references || storedProp.ReferenceImages != references {
		t.Fatalf("create did not persist reference images: character=%q prop=%q", storedCharacter.ReferenceImages, storedProp.ReferenceImages)
	}

	if resp := performRequest(router, http.MethodPost, "/api/v1/characters/"+itoa(characterID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("character image status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := performRequest(router, http.MethodPost, "/api/v1/props/"+itoa(propID)+"/generate-image", `{"episode_id":`+itoa(episodeID)+`}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("prop image status=%d body=%s", resp.Code, resp.Body.String())
	}

	var characterGeneration models.ImageGeneration
	if err := db.DB.Where("character_id = ?", characterID).Order("id desc").First(&characterGeneration).Error; err != nil {
		t.Fatal(err)
	}
	var propGeneration models.ImageGeneration
	if err := db.DB.Where("prop_id = ?", propID).Order("id desc").First(&propGeneration).Error; err != nil {
		t.Fatal(err)
	}
	if characterGeneration.ReferenceImages != references {
		t.Fatalf("character generation dropped reference images: %q", characterGeneration.ReferenceImages)
	}
	if propGeneration.ReferenceImages != references {
		t.Fatalf("prop generation dropped reference images: %q", propGeneration.ReferenceImages)
	}

	batch := performRequest(router, http.MethodPost, "/api/v1/characters/batch-generate-images",
		`{"episode_id":`+itoa(episodeID)+`,"character_ids":[`+itoa(characterID)+`]}`, nil)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch image status=%d body=%s", batch.Code, batch.Body.String())
	}
	var batchGeneration models.ImageGeneration
	if err := db.DB.Where("character_id = ?", characterID).Order("id desc").First(&batchGeneration).Error; err != nil {
		t.Fatal(err)
	}
	if batchGeneration.ReferenceImages != references {
		t.Fatalf("batch generation dropped reference images: %q", batchGeneration.ReferenceImages)
	}
}

// Storyboard character bindings live in storyboard_characters and are the only
// source of truth. Create, update and list must round-trip them so the
// workbench picker never has to keep its own copy of the relationship.
func TestStoryboardCharacterBindingsRoundTripThroughAPI(t *testing.T) {
	_, router := testServerRouter(t)
	imageConfig := createMockConfig(t, router, "image")
	videoConfig := createMockConfig(t, router, "video")
	audioConfig := createMockConfig(t, router, "audio")
	drama := requestData(t, router, http.MethodPost, "/api/v1/dramas", `{"title":"character bindings","total_episodes":1}`)
	dramaID := uintField(t, drama, "id")
	episode := requestData(t, router, http.MethodPost, "/api/v1/episodes", `{"drama_id":`+itoa(dramaID)+`,"image_config_id":`+itoa(imageConfig)+`,"video_config_id":`+itoa(videoConfig)+`,"audio_config_id":`+itoa(audioConfig)+`}`)
	episodeID := uintField(t, episode, "id")

	first := uintField(t, requestData(t, router, http.MethodPost, "/api/v1/characters",
		`{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"林舟"}`), "id")
	second := uintField(t, requestData(t, router, http.MethodPost, "/api/v1/characters",
		`{"drama_id":`+itoa(dramaID)+`,"episode_id":`+itoa(episodeID)+`,"name":"沈宁"}`), "id")

	storyboard := requestData(t, router, http.MethodPost, "/api/v1/storyboards",
		`{"episode_id":`+itoa(episodeID)+`,"title":"对峙","character_ids":[`+itoa(first)+`]}`)
	storyboardID := uintField(t, storyboard, "id")

	assertBoundCharacters := func(stage string, want ...uint) {
		t.Helper()
		shots := requestSlice(t, router, "/api/v1/episodes/"+itoa(episodeID)+"/storyboards")
		for _, shot := range shots {
			if uint(shot["id"].(float64)) != storyboardID {
				continue
			}
			raw, ok := shot["character_ids"].([]any)
			if !ok {
				t.Fatalf("%s: storyboard response is missing character_ids: %#v", stage, shot)
			}
			got := make([]uint, 0, len(raw))
			for _, value := range raw {
				got = append(got, uint(value.(float64)))
			}
			if len(got) != len(want) {
				t.Fatalf("%s: bound characters = %v, want %v", stage, got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("%s: bound characters = %v, want %v", stage, got, want)
				}
			}
			return
		}
		t.Fatalf("%s: storyboard %d missing from episode listing", stage, storyboardID)
	}

	assertBoundCharacters("after create", first)

	// Replacing the selection must persist both ids.
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID),
		`{"character_ids":[`+itoa(first)+`,`+itoa(second)+`]}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.Code, resp.Body.String())
	}
	assertBoundCharacters("after adding a character", first, second)

	// Editing an unrelated field must not disturb existing bindings.
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID),
		`{"atmosphere":"紧绷"}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("unrelated update status=%d body=%s", resp.Code, resp.Body.String())
	}
	assertBoundCharacters("after unrelated field edit", first, second)

	// An explicit empty array clears the relationship.
	if resp := performRequest(router, http.MethodPut, "/api/v1/storyboards/"+itoa(storyboardID),
		`{"character_ids":[]}`, nil); resp.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", resp.Code, resp.Body.String())
	}
	assertBoundCharacters("after clearing")

	var remaining int64
	if err := db.DB.Model(&models.StoryboardCharacter{}).Where("storyboard_id = ?", storyboardID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("expected storyboard_characters rows to be removed, found %d", remaining)
	}
}
