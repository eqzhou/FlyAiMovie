package adapters

import "context"

type AIConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

type ProviderRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    any
}

type ImageGenInput struct {
	Prompt          string
	Size            string
	ReferenceImages []string
	FrameType       string
}

type ImageGenResult struct {
	IsAsync  bool
	TaskID   string
	ImageURL string
	Base64   string
	MimeType string
}

type ImagePollResult struct {
	Status   string // pending|processing|completed|failed
	ImageURL string
	Error    string
}

type VideoGenInput struct {
	Prompt             string
	Duration           int
	AspectRatio        string
	ReferenceMode      string
	ImageURL           string
	FirstFrameURL      string
	LastFrameURL       string
	ReferenceImageURLs []string
}

type VideoGenResult struct {
	IsAsync  bool
	TaskID   string
	VideoURL string
}

type VideoPollResult struct {
	Status   string
	VideoURL string
	Error    string
}

type TTSInput struct {
	Text    string
	VoiceID string
	Model   string
}

type TTSResult struct {
	AudioHex   string
	AudioBytes []byte
	Format     string
}

type ImageProvider interface {
	Name() string
	Generate(ctx context.Context, cfg AIConfig, in ImageGenInput) (*ImageGenResult, error)
	Poll(ctx context.Context, cfg AIConfig, taskID string) (*ImagePollResult, error)
}

type VideoProvider interface {
	Name() string
	Generate(ctx context.Context, cfg AIConfig, in VideoGenInput) (*VideoGenResult, error)
	Poll(ctx context.Context, cfg AIConfig, taskID string) (*VideoPollResult, error)
}

type TTSProvider interface {
	Name() string
	Generate(ctx context.Context, cfg AIConfig, in TTSInput) (*TTSResult, error)
}
