package adapters

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MiniMaxTTSAdapter struct{}

type VoiceInfo struct {
	ID           string
	Name         string
	Language     string
	Description  string
	Capabilities string
}

func (a *MiniMaxTTSAdapter) Name() string { return "minimax" }

func ListMiniMaxVoices(ctx context.Context, cfg AIConfig) ([]VoiceInfo, error) {
	endpoint, err := miniMaxVoiceEndpoint(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	data, err := providerJSON(ctx, http.MethodGet, endpoint, cfg.APIKey, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("minimax voices: %w", err)
	}
	if err := providerBaseError("minimax", data); err != nil {
		return nil, err
	}
	voices := make([]VoiceInfo, 0)
	for _, key := range []string{"system_voice", "voice_cloning", "voice_generation"} {
		items, _ := data[key].([]any)
		for _, item := range items {
			row, _ := item.(map[string]any)
			id := firstString(row, "voice_id", "id")
			if id == "" {
				continue
			}
			voices = append(voices, VoiceInfo{ID: id, Name: defaultString(firstString(row, "voice_name", "name"), id),
				Language: firstString(row, "language"), Description: firstString(row, "description"), Capabilities: key})
		}
	}
	if len(voices) == 0 {
		return nil, fmt.Errorf("minimax voices: response contains no voices")
	}
	return voices, nil
}

func (a *MiniMaxTTSAdapter) Generate(ctx context.Context, cfg AIConfig, in TTSInput) (*TTSResult, error) {
	endpoint, err := miniMaxTTSEndpoint(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("minimax tts: API key is required")
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, fmt.Errorf("minimax tts: text is required")
	}
	model := in.Model
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = "speech-02-hd"
	}
	voiceID := in.VoiceID
	if voiceID == "" {
		voiceID = "male-qn-qingse"
	}
	body := map[string]any{
		"model":  model,
		"text":   text,
		"stream": false,
		"voice_setting": map[string]any{
			"voice_id": voiceID,
			"speed":    1,
			"vol":      1,
			"pitch":    0,
		},
		"audio_setting": map[string]any{
			"sample_rate": 32000,
			"bitrate":     128000,
			"format":      "mp3",
			"channel":     1,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("minimax tts: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := providerHTTPClient(120 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("minimax tts: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("minimax tts: provider request failed with HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Data struct {
			Audio string `json:"audio"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int `json:"status_code"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("minimax tts: decode response: %w", err)
	}
	if parsed.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax tts: provider status %d", parsed.BaseResp.StatusCode)
	}
	audioHex := strings.TrimSpace(parsed.Data.Audio)
	if audioHex == "" {
		return nil, fmt.Errorf("minimax tts: response contains no audio")
	}
	audioBytes, err := hex.DecodeString(audioHex)
	if err != nil {
		return nil, fmt.Errorf("minimax tts: audio is not valid hex: %w", err)
	}
	return &TTSResult{AudioHex: audioHex, AudioBytes: audioBytes, Format: "mp3"}, nil
}

func miniMaxTTSEndpoint(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("minimax tts: base URL is required")
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("minimax tts: invalid base URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		u.Path = path + "/t2a_v2"
	} else {
		u.Path = path + "/v1/t2a_v2"
	}
	return u.String(), nil
}

func miniMaxVoiceEndpoint(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("minimax voices: base URL is required")
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("minimax voices: invalid base URL")
	}
	u.RawQuery, u.Fragment = "", ""
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		u.Path = path + "/get_voice"
	} else {
		u.Path = path + "/v1/get_voice"
	}
	query := u.Query()
	query.Set("voice_type", "all")
	u.RawQuery = query.Encode()
	return u.String(), nil
}
