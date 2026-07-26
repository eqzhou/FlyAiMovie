package models

// Core domain models for FlyAiMovie short-drama pipeline.
// Designed for feature parity with common AI drama workflows (clean-room).

type Drama struct {
	OrganizationID uint    `gorm:"not null;default:0;index" json:"-"`
	ID             uint    `gorm:"primaryKey" json:"id"`
	Title          string  `gorm:"not null" json:"title"`
	Description    string  `json:"description"`
	Genre          string  `json:"genre"`
	Style          string  `gorm:"default:realistic" json:"style"`
	TotalEpisodes  int     `gorm:"default:1" json:"total_episodes"`
	TotalDuration  int     `gorm:"default:0" json:"total_duration"`
	Status         string  `gorm:"not null;default:draft" json:"status"`
	Thumbnail      string  `json:"thumbnail"`
	Tags           string  `json:"tags"` // JSON array string
	Metadata       string  `json:"metadata"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type Episode struct {
	OrganizationID uint    `gorm:"not null;default:0;index" json:"-"`
	ID             uint    `gorm:"primaryKey" json:"id"`
	DramaID        uint    `gorm:"not null;index" json:"drama_id"`
	EpisodeNumber  int     `gorm:"not null" json:"episode_number"`
	Title          string  `gorm:"not null" json:"title"`
	Content        string  `json:"content"`
	ScriptContent  string  `json:"script_content"`
	Description    string  `json:"description"`
	Duration       int     `gorm:"default:0" json:"duration"`
	Status         string  `gorm:"default:draft" json:"status"`
	VideoURL       string  `json:"video_url"`
	Thumbnail      string  `json:"thumbnail"`
	ImageConfigID  *uint   `json:"image_config_id"`
	VideoConfigID  *uint   `json:"video_config_id"`
	AudioConfigID  *uint   `json:"audio_config_id"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type Character struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	DramaID         uint    `gorm:"not null;index" json:"drama_id"`
	Name            string  `gorm:"not null" json:"name"`
	Role            string  `json:"role"`
	Description     string  `json:"description"`
	Appearance      string  `json:"appearance"`
	Personality     string  `json:"personality"`
	VoiceStyle      string  `json:"voice_style"`
	ImageURL        string  `json:"image_url"`
	ReferenceImages string  `json:"reference_images"`
	SeedValue       string  `json:"seed_value"`
	SortOrder       int     `json:"sort_order"`
	LocalPath       string  `json:"local_path"`
	VoiceSampleURL  string  `json:"voice_sample_url"`
	VoiceProvider   string  `json:"voice_provider"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	DeletedAt       *string `json:"deleted_at,omitempty"`
}

type CharacterTemplate struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	Name            string  `gorm:"not null" json:"name"`
	Role            string  `json:"role"`
	Description     string  `json:"description"`
	Appearance      string  `json:"appearance"`
	Personality     string  `json:"personality"`
	VoiceStyle      string  `json:"voice_style"`
	VoiceProvider   string  `json:"voice_provider"`
	ImageURL        string  `json:"image_url"`
	ReferenceImages string  `json:"reference_images"`
	LocalPath       string  `json:"local_path"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	DeletedAt       *string `json:"deleted_at,omitempty"`
}

type EpisodeCharacter struct {
	OrganizationID uint   `gorm:"not null;default:0;index" json:"-"`
	ID             uint   `gorm:"primaryKey" json:"id"`
	EpisodeID      uint   `gorm:"not null;index" json:"episode_id"`
	CharacterID    uint   `gorm:"not null;index" json:"character_id"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
}

type EpisodeScene struct {
	OrganizationID uint   `gorm:"not null;default:0;index" json:"-"`
	ID             uint   `gorm:"primaryKey" json:"id"`
	EpisodeID      uint   `gorm:"not null;index" json:"episode_id"`
	SceneID        uint   `gorm:"not null;index" json:"scene_id"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
}

type Scene struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	DramaID         uint    `gorm:"not null;index" json:"drama_id"`
	EpisodeID       *uint   `json:"episode_id"`
	Location        string  `gorm:"not null" json:"location"`
	Time            string  `gorm:"not null" json:"time"`
	Prompt          string  `gorm:"not null" json:"prompt"`
	StoryboardCount int     `gorm:"default:1" json:"storyboard_count"`
	ImageURL        string  `json:"image_url"`
	Status          string  `gorm:"default:pending" json:"status"`
	LocalPath       string  `json:"local_path"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	DeletedAt       *string `json:"deleted_at,omitempty"`
}

type Storyboard struct {
	OrganizationID   uint    `gorm:"not null;default:0;index" json:"-"`
	ID               uint    `gorm:"primaryKey" json:"id"`
	EpisodeID        uint    `gorm:"not null;index" json:"episode_id"`
	SceneID          *uint   `json:"scene_id"`
	StoryboardNumber int     `gorm:"not null" json:"storyboard_number"`
	Title            string  `json:"title"`
	Location         string  `json:"location"`
	Time             string  `json:"time"`
	ShotType         string  `json:"shot_type"`
	Angle            string  `json:"angle"`
	Movement         string  `json:"movement"`
	Action           string  `json:"action"`
	Result           string  `json:"result"`
	Atmosphere       string  `json:"atmosphere"`
	ImagePrompt      string  `json:"image_prompt"`
	VideoPrompt      string  `json:"video_prompt"`
	BGMPrompt        string  `json:"bgm_prompt"`
	SoundEffect      string  `json:"sound_effect"`
	Dialogue         string  `json:"dialogue"`
	Description      string  `json:"description"`
	Duration         int     `gorm:"default:0" json:"duration"`
	ComposedImage    string  `json:"composed_image"`
	FirstFrameImage  string  `json:"first_frame_image"`
	LastFrameImage   string  `json:"last_frame_image"`
	ReferenceImages  string  `json:"reference_images"`
	VideoURL         string  `json:"video_url"`
	TTSAudioURL      string  `json:"tts_audio_url"`
	SubtitleURL      string  `json:"subtitle_url"`
	ComposedVideoURL string  `json:"composed_video_url"`
	Status           string  `gorm:"default:pending" json:"status"`
	CreatedAt        string  `gorm:"not null" json:"created_at"`
	UpdatedAt        string  `gorm:"not null" json:"updated_at"`
	DeletedAt        *string `json:"deleted_at,omitempty"`
}

type StoryboardCharacter struct {
	OrganizationID uint `gorm:"not null;default:0;index" json:"-"`
	StoryboardID   uint `gorm:"primaryKey" json:"storyboard_id"`
	CharacterID    uint `gorm:"primaryKey" json:"character_id"`
}

type AIServiceConfig struct {
	OrganizationID uint   `gorm:"not null;default:0;index" json:"-"`
	ID             uint   `gorm:"primaryKey" json:"id"`
	ServiceType    string `gorm:"not null" json:"service_type"` // text|image|video|audio
	Provider       string `json:"provider"`
	Name           string `gorm:"not null" json:"name"`
	BaseURL        string `gorm:"not null" json:"base_url"`
	APIKey         string `gorm:"not null" json:"api_key"`
	Model          string `json:"model"`
	Endpoint       string `json:"endpoint"`
	QueryEndpoint  string `json:"query_endpoint"`
	Priority       int    `gorm:"default:0" json:"priority"`
	IsDefault      bool   `gorm:"default:false" json:"is_default"`
	IsActive       bool   `json:"is_active"`
	Settings       string `json:"settings"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
	UpdatedAt      string `gorm:"not null" json:"updated_at"`
}

type AIServiceProvider struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Name         string `gorm:"not null" json:"name"`
	DisplayName  string `json:"display_name"`
	ServiceType  string `gorm:"not null" json:"service_type"`
	Provider     string `gorm:"not null" json:"provider"`
	DefaultURL   string `json:"default_url"`
	PresetModels string `json:"preset_models"`
	Description  string `json:"description"`
	IsActive     bool   `json:"is_active"`
	CreatedAt    string `gorm:"not null" json:"created_at"`
	UpdatedAt    string `gorm:"not null" json:"updated_at"`
}

type AIVoice struct {
	OrganizationID uint   `gorm:"not null;default:0;uniqueIndex:idx_org_voice" json:"-"`
	ID             uint   `gorm:"primaryKey" json:"id"`
	VoiceID        string `gorm:"uniqueIndex:idx_org_voice;not null" json:"voice_id"`
	VoiceName      string `gorm:"not null" json:"voice_name"`
	Description    string `json:"description"`
	Language       string `json:"language"`
	Provider       string `gorm:"not null" json:"provider"`
	Capabilities   string `json:"capabilities"`
	IsActive       bool   `gorm:"not null" json:"is_active"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
	UpdatedAt      string `gorm:"not null" json:"updated_at"`
}

type AgentConfig struct {
	OrganizationID uint     `gorm:"not null;default:0;index" json:"-"`
	ID             uint     `gorm:"primaryKey" json:"id"`
	AgentType      string   `gorm:"not null;index" json:"agent_type"`
	Name           string   `gorm:"not null" json:"name"`
	Description    string   `json:"description"`
	Model          string   `json:"model"`
	SystemPrompt   string   `json:"system_prompt"`
	Temperature    *float64 `json:"temperature"`
	MaxTokens      *int     `json:"max_tokens"`
	MaxIterations  *int     `json:"max_iterations"`
	IsActive       bool     `json:"is_active"`
	CreatedAt      string   `gorm:"not null" json:"created_at"`
	UpdatedAt      string   `gorm:"not null" json:"updated_at"`
	DeletedAt      *string  `json:"deleted_at,omitempty"`
}

type PromptTemplate struct {
	OrganizationID uint    `gorm:"not null;default:0;uniqueIndex:idx_org_prompt_key" json:"-"`
	ID             uint    `gorm:"primaryKey" json:"id"`
	Key            string  `gorm:"not null;uniqueIndex:idx_org_prompt_key" json:"key"`
	Name           string  `gorm:"not null" json:"name"`
	Category       string  `gorm:"not null;index" json:"category"`
	Description    string  `json:"description"`
	Content        string  `gorm:"not null" json:"content"`
	VariablesJSON  string  `gorm:"not null;default:'[]'" json:"variables_json"`
	Version        int     `gorm:"not null;default:1" json:"version"`
	IsActive       bool    `gorm:"not null" json:"is_active"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type PromptTemplateRevision struct {
	OrganizationID   uint   `gorm:"not null;default:0;uniqueIndex:idx_org_prompt_revision" json:"-"`
	ID               uint   `gorm:"primaryKey" json:"id"`
	PromptTemplateID uint   `gorm:"not null;uniqueIndex:idx_org_prompt_revision;index" json:"template_id"`
	Version          int    `gorm:"not null;uniqueIndex:idx_org_prompt_revision" json:"version"`
	Key              string `gorm:"not null" json:"key"`
	Name             string `gorm:"not null" json:"name"`
	Category         string `gorm:"not null" json:"category"`
	Description      string `json:"description"`
	Content          string `gorm:"not null" json:"content"`
	VariablesJSON    string `gorm:"not null;default:'[]'" json:"variables_json"`
	IsActive         bool   `gorm:"not null" json:"is_active"`
	CreatedAt        string `gorm:"not null" json:"created_at"`
}

type ImageGeneration struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	JobID           *uint   `gorm:"index" json:"job_id,omitempty"`
	ConfigID        *uint   `gorm:"index" json:"config_id,omitempty"`
	StoryboardID    *uint   `json:"storyboard_id"`
	DramaID         *uint   `json:"drama_id"`
	SceneID         *uint   `json:"scene_id"`
	CharacterID     *uint   `json:"character_id"`
	PropID          *uint   `json:"prop_id"`
	ImageType       string  `json:"image_type"`
	FrameType       string  `json:"frame_type"`
	Provider        string  `json:"provider"`
	Prompt          string  `json:"prompt"`
	NegativePrompt  string  `json:"negative_prompt"`
	Model           string  `json:"model"`
	Size            string  `json:"size"`
	Quality         string  `json:"quality"`
	Style           string  `json:"style"`
	Steps           int     `json:"steps"`
	CFGScale        float64 `json:"cfg_scale"`
	Seed            int     `json:"seed"`
	ImageURL        string  `json:"image_url"`
	LocalPath       string  `json:"local_path"`
	Status          string  `gorm:"default:pending" json:"status"`
	TaskID          string  `json:"task_id"`
	ErrorMsg        string  `json:"error_msg"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	ReferenceImages string  `json:"reference_images"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	CompletedAt     *string `json:"completed_at,omitempty"`
}

type VideoGeneration struct {
	OrganizationID     uint    `gorm:"not null;default:0;index" json:"-"`
	ID                 uint    `gorm:"primaryKey" json:"id"`
	JobID              *uint   `gorm:"index" json:"job_id,omitempty"`
	ConfigID           *uint   `gorm:"index" json:"config_id,omitempty"`
	StoryboardID       *uint   `json:"storyboard_id"`
	DramaID            *uint   `json:"drama_id"`
	Provider           string  `json:"provider"`
	Prompt             string  `json:"prompt"`
	Model              string  `json:"model"`
	ImageGenID         *uint   `json:"image_gen_id"`
	ReferenceMode      string  `json:"reference_mode"`
	ImageURL           string  `json:"image_url"`
	FirstFrameURL      string  `json:"first_frame_url"`
	LastFrameURL       string  `json:"last_frame_url"`
	ReferenceImageURLs string  `json:"reference_image_urls"`
	Duration           int     `json:"duration"`
	FPS                int     `json:"fps"`
	Resolution         string  `json:"resolution"`
	AspectRatio        string  `json:"aspect_ratio"`
	Style              string  `json:"style"`
	MotionLevel        int     `json:"motion_level"`
	CameraMotion       string  `json:"camera_motion"`
	Seed               int     `json:"seed"`
	VideoURL           string  `json:"video_url"`
	LocalPath          string  `json:"local_path"`
	Status             string  `gorm:"default:pending" json:"status"`
	TaskID             string  `json:"task_id"`
	ErrorMsg           string  `json:"error_msg"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	CreatedAt          string  `gorm:"not null" json:"created_at"`
	UpdatedAt          string  `gorm:"not null" json:"updated_at"`
	CompletedAt        *string `json:"completed_at,omitempty"`
	DeletedAt          *string `json:"deleted_at,omitempty"`
}

type VideoMerge struct {
	OrganizationID uint    `gorm:"not null;default:0;index" json:"-"`
	ID             uint    `gorm:"primaryKey" json:"id"`
	EpisodeID      *uint   `json:"episode_id"`
	DramaID        *uint   `json:"drama_id"`
	Title          string  `json:"title"`
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	Status         string  `gorm:"default:pending" json:"status"`
	Scenes         string  `json:"scenes"`
	MergedURL      string  `json:"merged_url"`
	Duration       int     `json:"duration"`
	TaskID         string  `json:"task_id"`
	ErrorMsg       string  `json:"error_msg"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	CompletedAt    *string `json:"completed_at,omitempty"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type Prop struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	DramaID         uint    `gorm:"not null;index" json:"drama_id"`
	Name            string  `gorm:"not null" json:"name"`
	Type            string  `json:"type"`
	Description     string  `json:"description"`
	Prompt          string  `json:"prompt"`
	ImageURL        string  `json:"image_url"`
	ReferenceImages string  `json:"reference_images"`
	LocalPath       string  `json:"local_path"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	DeletedAt       *string `json:"deleted_at,omitempty"`
}

type Asset struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	DramaID         *uint   `json:"drama_id"`
	EpisodeID       *uint   `json:"episode_id"`
	StoryboardID    *uint   `json:"storyboard_id"`
	StoryboardNum   *int    `json:"storyboard_num"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Type            string  `json:"type"`
	Category        string  `json:"category"`
	URL             string  `json:"url"`
	ThumbnailURL    string  `json:"thumbnail_url"`
	LocalPath       string  `json:"local_path"`
	FileSize        int64   `json:"file_size"`
	MimeType        string  `json:"mime_type"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Duration        int     `json:"duration"`
	DurationSeconds float64 `json:"duration_seconds"`
	FrameRate       float64 `json:"frame_rate"`
	Codec           string  `json:"codec"`
	ProbeStatus     string  `gorm:"default:pending" json:"probe_status"`
	ProbeError      string  `json:"probe_error"`
	ContentHash     string  `gorm:"index" json:"content_hash"`
	ReferenceCount  int     `gorm:"default:1" json:"reference_count"`
	Format          string  `json:"format"`
	ImageGenID      *uint   `json:"image_gen_id"`
	VideoGenID      *uint   `json:"video_gen_id"`
	GridHistoryID   *uint   `gorm:"index" json:"grid_history_id"`
	IsFavorite      bool    `gorm:"default:false" json:"is_favorite"`
	ViewCount       int     `gorm:"default:0" json:"view_count"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	DeletedAt       *string `json:"deleted_at,omitempty"`
}

// GridHistory stores grid generation jobs and split results for the workbench.
type GridHistory struct {
	OrganizationID  uint    `gorm:"not null;default:0;index" json:"-"`
	ID              uint    `gorm:"primaryKey" json:"id"`
	DramaID         *uint   `json:"drama_id"`
	EpisodeID       *uint   `json:"episode_id"`
	Mode            string  `json:"mode"` // first_frame|first_last|multi_ref
	SplitFrameType  string  `json:"split_frame_type"`
	Rows            int     `json:"rows"`
	Cols            int     `json:"cols"`
	Prompt          string  `json:"prompt"`
	CellPrompts     string  `json:"cell_prompts"` // JSON array
	ImageGenID      *uint   `json:"image_gen_id"`
	ImageURL        string  `json:"image_url"`
	CellsJSON       string  `json:"cells_json"` // JSON array of cell urls
	StoryboardIDs   string  `json:"storyboard_ids"`
	AssignmentsJSON string  `json:"assignments_json"` // JSON array of persisted cell-to-storyboard assignments
	CellsVerified   bool    `gorm:"not null;default:false" json:"cells_verified"`
	Status          string  `gorm:"default:pending" json:"status"`
	ErrorMsg        string  `json:"error_msg"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
	CompletedAt     *string `json:"completed_at,omitempty"`
}

// WebhookReceipt makes signed provider callbacks idempotent across restarts.
type WebhookReceipt struct {
	EventID       string `gorm:"primaryKey;size:128" json:"event_id"`
	SignatureHash string `gorm:"not null;size:64" json:"-"`
	ReceivedAt    string `gorm:"not null" json:"received_at"`
}

type MediaMigration struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;default:0;index;uniqueIndex:idx_media_migration_target" json:"organization_id"`
	TargetType     string  `gorm:"not null;uniqueIndex:idx_media_migration_target" json:"target_type"`
	TargetID       uint    `gorm:"not null;uniqueIndex:idx_media_migration_target" json:"target_id"`
	SourceURL      string  `gorm:"not null" json:"source_url"`
	LocalPath      string  `json:"local_path"`
	Status         string  `gorm:"not null;default:pending;index" json:"status"`
	Attempts       int     `gorm:"not null;default:0" json:"attempts"`
	LastError      string  `json:"last_error"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
	CompletedAt    *string `json:"completed_at,omitempty"`
}
