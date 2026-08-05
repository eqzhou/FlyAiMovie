package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/adapters"
	"github.com/eqzhou/flyaimovie/internal/services/ai"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Server) registerVoices(api *gin.RouterGroup) {
	api.GET("/ai-voices", s.listVoices)
	api.POST("/ai-voices/sync", s.syncVoices)
	api.POST("/ai-voices/:voiceID/preview", s.previewVoice)
}

func (s *Server) listVoices(c *gin.Context) {
	provider := strings.TrimSpace(c.Query("provider"))
	if len(provider) > 80 {
		response.BadRequest(c, "invalid provider")
		return
	}
	var rows []models.AIVoice
	query := organizationDB(c).Model(&models.AIVoice{})
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if c.Query("include_inactive") != "1" {
		query = query.Where("is_active = ?", true)
	}
	if err := query.Order("is_active DESC, provider, voice_name, voice_id").Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to load voices")
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, gin.H{"voice_id": row.VoiceID, "voice_name": row.VoiceName, "description": row.Description,
			"language": row.Language, "provider": row.Provider, "capabilities": row.Capabilities, "is_active": row.IsActive})
	}
	response.Success(c, out)
}

func (s *Server) syncVoices(c *gin.Context) {
	organizationID := currentOrganizationID(c)
	var cfg *ai.ServiceConfig
	var err error
	if organizationID == 0 {
		cfg, err = ai.GetActiveConfig("audio", nil)
	} else {
		cfg, err = ai.GetOrganizationConfig(organizationID, "audio", nil)
	}
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	voices := []adapters.VoiceInfo{}
	message := "MiniMax voices synchronized"
	if cfg.Provider == "mock" {
		voices = []adapters.VoiceInfo{{ID: "male-qn-qingse", Name: "青涩青年", Language: "中文", Capabilities: "mock"},
			{ID: "male-qn-jingying", Name: "精英青年", Language: "中文", Capabilities: "mock"}, {ID: "female-shaonv", Name: "少女", Language: "中文", Capabilities: "mock"},
			{ID: "female-yujie", Name: "御姐", Language: "中文", Capabilities: "mock"}, {ID: "presenter_male", Name: "男性主持人", Language: "中文", Capabilities: "mock"},
			{ID: "presenter_female", Name: "女性主持人", Language: "中文", Capabilities: "mock"}}
		message = "Mock voices seeded"
	} else if cfg.Provider == "minimax" {
		voices, err = adapters.ListMiniMaxVoices(c.Request.Context(), adapters.AIConfig{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model})
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	} else {
		response.BadRequest(c, "active audio provider does not support voice synchronization")
		return
	}
	ts := response.Now()
	seen := make([]string, 0, len(voices))
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		for _, voice := range voices {
			if strings.TrimSpace(voice.ID) == "" {
				continue
			}
			seen = append(seen, voice.ID)
			row := models.AIVoice{OrganizationID: organizationID, VoiceID: voice.ID, VoiceName: voice.Name, Description: voice.Description,
				Language: voice.Language, Provider: cfg.Provider, Capabilities: voice.Capabilities, IsActive: true, CreatedAt: ts, UpdatedAt: ts}
			var existing models.AIVoice
			findErr := tx.Where("organization_id = ? AND voice_id = ?", organizationID, voice.ID).First(&existing).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				continue
			}
			if findErr != nil {
				return findErr
			}
			if err := tx.Model(&existing).Updates(map[string]any{"voice_name": row.VoiceName, "description": row.Description, "language": row.Language,
				"provider": row.Provider, "capabilities": row.Capabilities, "is_active": true, "updated_at": ts}).Error; err != nil {
				return err
			}
		}
		query := tx.Model(&models.AIVoice{}).Where("organization_id = ? AND provider = ?", organizationID, cfg.Provider)
		if len(seen) > 0 {
			query = query.Where("voice_id NOT IN ?", seen)
		}
		return query.Updates(map[string]any{"is_active": false, "updated_at": ts}).Error
	})
	if err != nil {
		response.ServerError(c, "failed to save synchronized voices")
		return
	}
	response.Success(c, gin.H{"count": len(seen), "message": message})
}

func (s *Server) previewVoice(c *gin.Context) {
	voiceID := strings.TrimSpace(c.Param("voiceID"))
	if voiceID == "" || len([]rune(voiceID)) > 200 {
		response.BadRequest(c, "invalid voice id")
		return
	}
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		response.BadRequest(c, "invalid JSON body")
		return
	}
	if err := rejectUnknownFields(raw, "text", "config_id"); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	encoded, _ := json.Marshal(raw)
	var body struct {
		Text     string `json:"text"`
		ConfigID uint   `json:"config_id"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		response.BadRequest(c, "invalid voice preview fields")
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" || len([]rune(body.Text)) > 200 {
		response.BadRequest(c, "preview text must contain 1 to 200 characters")
		return
	}
	organizationID := currentOrganizationID(c)
	var voice models.AIVoice
	if err := organizationDB(c).Where("voice_id = ? AND is_active = ?", voiceID, true).First(&voice).Error; err != nil {
		response.NotFound(c, "active voice not found")
		return
	}
	var configRow models.AIServiceConfig
	query := organizationDB(c).Where("service_type = ? AND provider = ? AND is_active = ?", "audio", voice.Provider, true)
	if body.ConfigID > 0 {
		query = query.Where("id = ?", body.ConfigID)
	} else {
		query = query.Order("is_default DESC, priority DESC, id DESC")
	}
	if err := query.First(&configRow).Error; err != nil {
		response.BadRequest(c, "no matching active audio config for voice provider")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	url, err := s.TTS.GenerateVoicePreviewOrganization(ctx, organizationID, body.Text, voice.VoiceID, &configRow.ID)
	if err != nil {
		response.BadRequest(c, "TTS preview failed: "+err.Error())
		return
	}
	response.Success(c, gin.H{"voice_id": voice.VoiceID, "provider": voice.Provider, "audio_url": url})
}
