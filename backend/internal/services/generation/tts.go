package generation

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/eqzhou/flyaimovie/internal/services/jobs"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ignoreSpeaker = regexp.MustCompile(`^(环境音|环境声|音效|效果音|sfx|sound ?effect|bgm|背景音|背景音乐|ambient)$`)
	ignoreText    = regexp.MustCompile(`^(无|无对白|无台词|无旁白|无需配音|none|null|n/a|环境音|音效|bgm|sfx)$`)
)

type TTSService struct {
	Store *storage.LocalStorage
	Cache *mediacache.Service
}

func (s *TTSService) GenerateForStoryboard(ctx context.Context, storyboardID uint, audioConfigID *uint) (string, error) {
	var sb models.Storyboard
	if err := db.DB.First(&sb, storyboardID).Error; err != nil {
		return "", err
	}
	return s.generateForStoryboard(ctx, &sb, audioConfigID, sb.OrganizationID, nil, 0, "")
}

func (s *TTSService) generateForStoryboard(ctx context.Context, sb *models.Storyboard, audioConfigID *uint, organizationID uint, jobService *jobs.Service, jobID uint, owner string) (string, error) {
	if sb == nil {
		return "", fmt.Errorf("storyboard is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	speaker, text, ignorable := parseDialogue(sb.Dialogue)
	if ignorable {
		return "", fmt.Errorf("no tts content")
	}
	voiceID := ""
	if speaker != "" {
		var ch models.Character
		// find character by name in episode characters
		var links []models.StoryboardCharacter
		db.DB.Where("storyboard_id = ?", sb.ID).Find(&links)
		for _, l := range links {
			var c models.Character
			query := db.DB.Where("id = ?", l.CharacterID)
			if organizationID != 0 {
				query = query.Where("organization_id = ?", organizationID)
			}
			if err := query.First(&c).Error; err == nil {
				if c.Name == speaker || strings.Contains(speaker, c.Name) {
					voiceID = c.VoiceStyle
					break
				}
				ch = c
			}
		}
		if voiceID == "" && ch.VoiceStyle != "" {
			voiceID = ch.VoiceStyle
		}
		// fallback search by drama characters via episode
		if voiceID == "" {
			var ep models.Episode
			query := db.DB.Where("id = ?", sb.EpisodeID)
			if organizationID != 0 {
				query = query.Where("organization_id = ?", organizationID)
			}
			if err := query.First(&ep).Error; err == nil {
				var chars []models.Character
				query := db.DB.Where("drama_id = ? AND deleted_at IS NULL", ep.DramaID)
				if organizationID != 0 {
					query = query.Where("organization_id = ?", organizationID)
				}
				query.Find(&chars)
				for _, c := range chars {
					if c.Name == speaker {
						voiceID = c.VoiceStyle
						break
					}
				}
			}
		}
	}

	cfg, err := ai.GetTaskConfigOrganization(organizationID, "audio", audioConfigID)
	if err != nil {
		return "", err
	}
	adapter := adapters.GetTTSAdapter(cfg.Provider)
	res, err := adapter.Generate(ctx, adapters.AIConfig{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
	}, adapters.TTSInput{Text: text, VoiceID: voiceID, Model: cfg.Model})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var audio []byte
	if len(res.AudioBytes) > 0 {
		audio = res.AudioBytes
	} else {
		return "", fmt.Errorf("empty audio bytes")
	}
	name := fmt.Sprintf("tts_%d_%s.mp3", sb.ID, uuid.NewString()[:8])
	rel, abs, err := s.Store.Save("audio", name, bytes.NewReader(audio))
	if err != nil {
		return "", err
	}
	_ = abs
	if err := ctx.Err(); err != nil {
		return "", err
	}
	url := s.Store.PublicURL(rel)
	canonicalPath, canonicalURL, contentHash, size, cacheErr := cacheGeneratedFile(s.Cache, s.Store, organizationID, "storyboard_tts", sb.ID, "audio", rel, url, "audio/mpeg")
	if cacheErr != nil {
		return "", cacheErr
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rel, url = canonicalPath, canonicalURL
	episodeID := sb.EpisodeID
	asset := models.Asset{OrganizationID: organizationID, EpisodeID: &episodeID, StoryboardID: &sb.ID, Name: "镜头配音", Type: "audio", Category: "tts", URL: url, LocalPath: rel, ContentHash: contentHash, FileSize: size}
	commit := func(database *gorm.DB) error {
		query := database.Model(&models.Storyboard{}).Where("id = ?", sb.ID)
		if organizationID != 0 {
			query = query.Where("organization_id = ?", organizationID)
		}
		if err := query.Updates(map[string]any{"tts_audio_url": url, "updated_at": response.Now()}).Error; err != nil {
			return err
		}
		return registerAssetWithDB(database, asset)
	}
	if jobID > 0 {
		if jobService == nil {
			return "", jobs.ErrTerminalJob
		}
		if err := jobService.SetSucceededOwnedWith(ctx, jobID, owner, `{"status":"completed"}`, commit); err != nil {
			return "", err
		}
	} else if err := db.DB.Transaction(commit); err != nil {
		return "", err
	}
	return url, nil
}

// GenerateForStoryboardOrganization validates the target before delegating to
// the existing provider implementation. Worker callers use this guard so a
// stale/crafted job cannot mutate a different tenant's storyboard.
func (s *TTSService) GenerateForStoryboardOrganization(ctx context.Context, organizationID, storyboardID uint, audioConfigID *uint) (string, error) {
	var sb models.Storyboard
	if err := db.DB.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, storyboardID).First(&sb).Error; err != nil {
		return "", err
	}
	return s.generateForStoryboard(ctx, &sb, audioConfigID, organizationID, nil, 0, "")
}

func (s *TTSService) GenerateForStoryboardOrganizationOwned(ctx context.Context, organizationID, storyboardID uint, audioConfigID *uint, jobService *jobs.Service, jobID uint, owner string) (string, error) {
	var sb models.Storyboard
	if err := db.DB.Where("organization_id = ? AND id = ? AND deleted_at IS NULL", organizationID, storyboardID).First(&sb).Error; err != nil {
		return "", err
	}
	return s.generateForStoryboard(ctx, &sb, audioConfigID, organizationID, jobService, jobID, owner)
}

func (s *TTSService) GenerateVoiceSample(ctx context.Context, characterName, voiceID string, audioConfigID *uint) (string, error) {
	return s.generateVoicePreview(ctx, 0, fmt.Sprintf("你好，我是%s，这是我的音色试听。", characterName), voiceID, audioConfigID)
}

func (s *TTSService) GenerateVoiceSampleOrganization(ctx context.Context, organizationID uint, characterName, voiceID string, audioConfigID *uint) (string, error) {
	return s.generateVoicePreview(ctx, organizationID, fmt.Sprintf("你好，我是%s，这是我的音色试听。", characterName), voiceID, audioConfigID)
}

func (s *TTSService) GenerateVoicePreviewOrganization(ctx context.Context, organizationID uint, text, voiceID string, audioConfigID *uint) (string, error) {
	return s.generateVoicePreview(ctx, organizationID, text, voiceID, audioConfigID)
}

func (s *TTSService) generateVoicePreview(ctx context.Context, organizationID uint, text, voiceID string, audioConfigID *uint) (string, error) {
	cfg, err := ai.GetTaskConfigOrganization(organizationID, "audio", audioConfigID)
	if err != nil {
		return "", err
	}
	adapter := adapters.GetTTSAdapter(cfg.Provider)
	res, err := adapter.Generate(ctx, adapters.AIConfig{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model,
	}, adapters.TTSInput{Text: text, VoiceID: voiceID, Model: cfg.Model})
	if err != nil {
		return "", err
	}
	if len(res.AudioBytes) == 0 {
		return "", fmt.Errorf("empty audio")
	}
	name := fmt.Sprintf("voice_%s_%s.mp3", time.Now().Format("150405"), uuid.NewString()[:6])
	rel, _, err := s.Store.Save("audio", name, bytes.NewReader(res.AudioBytes))
	if err != nil {
		return "", err
	}
	return s.Store.PublicURL(rel), nil
}

func parseDialogue(dialogue string) (speaker, pure string, ignorable bool) {
	raw := strings.TrimSpace(dialogue)
	if raw == "" {
		return "", "", true
	}
	re := regexp.MustCompile(`^(.+?)[:：]`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		speaker = strings.TrimSpace(regexp.MustCompile(`[（(].+?[)）]`).ReplaceAllString(m[1], ""))
	}
	pure = strings.TrimSpace(regexp.MustCompile(`^.+?[:：]\s*`).ReplaceAllString(raw, ""))
	pure = strings.TrimSpace(regexp.MustCompile(`[（(].+?[)）]`).ReplaceAllString(pure, ""))
	if (speaker != "" && ignoreSpeaker.MatchString(speaker)) || pure == "" || ignoreText.MatchString(pure) {
		return speaker, pure, true
	}
	return speaker, pure, false
}

func HasTTSContent(dialogue string) bool {
	_, _, ignorable := parseDialogue(dialogue)
	return !ignorable
}

// EnsureLocalFile resolves a public /static URL or relative path to an absolute local path.
func EnsureLocalFile(store *storage.LocalStorage, publicOrRel string) (string, error) {
	if publicOrRel == "" {
		return "", fmt.Errorf("empty path")
	}
	return store.Resolve(publicOrRel)
}
