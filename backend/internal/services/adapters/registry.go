package adapters

import (
	"context"
	"fmt"
	"strings"
)

var imageAdapters = map[string]ImageProvider{
	"mock":       &MockImageAdapter{},
	"openai":     &OpenAICompatImageAdapter{ProviderName: "openai"},
	"chatfire":   &OpenAICompatImageAdapter{ProviderName: "chatfire"},
	"gemini":     &GeminiImageAdapter{},
	"minimax":    &MiniMaxImageAdapter{},
	"volcengine": &VolcengineImageAdapter{},
	"ali":        &DashScopeImageAdapter{},
}

var videoAdapters = map[string]VideoProvider{
	"mock":       &MockVideoAdapter{},
	"openai":     &OpenAIVideoAdapter{},
	"minimax":    &MiniMaxVideoAdapter{},
	"volcengine": &VolcengineVideoAdapter{},
	"vidu":       &ViduVideoAdapter{},
	"ali":        &AliyunVideoAdapter{},
}

var ttsAdapters = map[string]TTSProvider{
	"mock":    &MockTTSAdapter{},
	"minimax": &MiniMaxTTSAdapter{},
}

func GetImageAdapter(provider string) ImageProvider {
	p := strings.ToLower(provider)
	if a, ok := imageAdapters[p]; ok {
		return a
	}
	return &unsupportedImageAdapter{name: p}
}

func GetVideoAdapter(provider string) VideoProvider {
	p := strings.ToLower(provider)
	if a, ok := videoAdapters[p]; ok {
		return a
	}
	return &unsupportedVideoAdapter{name: p}
}

func GetTTSAdapter(provider string) TTSProvider {
	p := strings.ToLower(provider)
	if a, ok := ttsAdapters[p]; ok {
		return a
	}
	return &unsupportedTTSAdapter{name: p}
}

func IsSupportedProvider(serviceType, provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch strings.ToLower(strings.TrimSpace(serviceType)) {
	case "text":
		return provider == "openai" || provider == "openai_local" || provider == "chatfire" || provider == "mock"
	case "image":
		_, ok := imageAdapters[provider]
		return ok
	case "video":
		_, ok := videoAdapters[provider]
		return ok
	case "audio":
		_, ok := ttsAdapters[provider]
		return ok
	default:
		return false
	}
}

type unsupportedImageAdapter struct{ name string }
type unsupportedVideoAdapter struct{ name string }
type unsupportedTTSAdapter struct{ name string }

func (a *unsupportedImageAdapter) Name() string { return a.name }
func (a *unsupportedImageAdapter) Generate(context.Context, AIConfig, ImageGenInput) (*ImageGenResult, error) {
	return nil, fmt.Errorf("unsupported image provider %q", a.name)
}
func (a *unsupportedImageAdapter) Poll(context.Context, AIConfig, string) (*ImagePollResult, error) {
	return nil, fmt.Errorf("unsupported image provider %q", a.name)
}
func (a *unsupportedVideoAdapter) Name() string { return a.name }
func (a *unsupportedVideoAdapter) Generate(context.Context, AIConfig, VideoGenInput) (*VideoGenResult, error) {
	return nil, fmt.Errorf("unsupported video provider %q", a.name)
}
func (a *unsupportedVideoAdapter) Poll(context.Context, AIConfig, string) (*VideoPollResult, error) {
	return nil, fmt.Errorf("unsupported video provider %q", a.name)
}
func (a *unsupportedTTSAdapter) Name() string { return a.name }
func (a *unsupportedTTSAdapter) Generate(context.Context, AIConfig, TTSInput) (*TTSResult, error) {
	return nil, fmt.Errorf("unsupported audio provider %q", a.name)
}
