package agents

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/textutil"
	"gorm.io/gorm"
)

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
	return textutil.UniquePositiveIDs(value, true)
}
