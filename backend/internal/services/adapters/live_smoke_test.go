package adapters

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live smoke tests intentionally stay skipped unless the operator exports real keys.
// They never run in default CI. Example:
//   SMOKE_OPENAI_KEY=... SMOKE_OPENAI_BASE_URL=https://api.openai.com go test ./internal/services/adapters -run TestLiveSmoke -count=1

func requireSmokeEnv(t *testing.T, keys ...string) map[string]string {
	t.Helper()
	values := map[string]string{}
	missing := make([]string, 0)
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			missing = append(missing, key)
			continue
		}
		values[key] = value
	}
	if len(missing) > 0 {
		t.Skipf("live smoke skipped; set %s", strings.Join(missing, ", "))
	}
	return values
}

func TestLiveSmokeOpenAIImage(t *testing.T) {
	env := requireSmokeEnv(t, "SMOKE_OPENAI_KEY")
	base := strings.TrimSpace(os.Getenv("SMOKE_OPENAI_BASE_URL"))
	if base == "" {
		base = "https://api.openai.com"
	}
	model := strings.TrimSpace(os.Getenv("SMOKE_OPENAI_IMAGE_MODEL"))
	if model == "" {
		model = "gpt-image-1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := (&OpenAIImageAdapter{}).Generate(ctx, AIConfig{BaseURL: base, APIKey: env["SMOKE_OPENAI_KEY"], Model: model}, ImageGenInput{Prompt: "simple red circle on white background, no text"})
	if err != nil {
		t.Fatalf("openai image smoke failed: %v", err)
	}
	if result == nil || (result.ImageURL == "" && result.Base64 == "" && !result.IsAsync) {
		t.Fatalf("unexpected openai image result %#v", result)
	}
}

func TestLiveSmokeMiniMaxTTS(t *testing.T) {
	env := requireSmokeEnv(t, "SMOKE_MINIMAX_KEY")
	base := strings.TrimSpace(os.Getenv("SMOKE_MINIMAX_BASE_URL"))
	if base == "" {
		base = "https://api.minimax.chat"
	}
	model := strings.TrimSpace(os.Getenv("SMOKE_MINIMAX_TTS_MODEL"))
	if model == "" {
		model = "speech-02-hd"
	}
	voice := strings.TrimSpace(os.Getenv("SMOKE_MINIMAX_VOICE_ID"))
	if voice == "" {
		voice = "male-qn-qingse"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := (&MiniMaxTTSAdapter{}).Generate(ctx, AIConfig{BaseURL: base, APIKey: env["SMOKE_MINIMAX_KEY"], Model: model}, TTSInput{Text: "FlyAiMovie smoke test.", VoiceID: voice})
	if err != nil {
		t.Fatalf("minimax tts smoke failed: %v", err)
	}
	if result == nil || (len(result.AudioBytes) == 0 && result.AudioHex == "") {
		t.Fatalf("unexpected minimax tts result %#v", result)
	}
}

func TestLiveSmokeViduVideoSubmit(t *testing.T) {
	env := requireSmokeEnv(t, "SMOKE_VIDU_KEY")
	base := strings.TrimSpace(os.Getenv("SMOKE_VIDU_BASE_URL"))
	if base == "" {
		base = "https://api.vidu.com"
	}
	model := strings.TrimSpace(os.Getenv("SMOKE_VIDU_MODEL"))
	if model == "" {
		model = "vidu2.0"
	}
	image := strings.TrimSpace(os.Getenv("SMOKE_VIDU_IMAGE_URL"))
	if image == "" {
		t.Skip("set SMOKE_VIDU_IMAGE_URL to a publicly reachable reference image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := (&ViduVideoAdapter{}).Generate(ctx, AIConfig{BaseURL: base, APIKey: env["SMOKE_VIDU_KEY"], Model: model}, VideoGenInput{Prompt: "gentle camera push in", Duration: 4, AspectRatio: "16:9", ImageURL: image})
	if err != nil {
		t.Fatalf("vidu video smoke failed: %v", err)
	}
	if result == nil || (!result.IsAsync && result.VideoURL == "") {
		t.Fatalf("unexpected vidu video result %#v", result)
	}
}
