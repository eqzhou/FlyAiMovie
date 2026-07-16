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

func (a *MiniMaxTTSAdapter) Name() string { return "minimax" }

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
		return nil, fmt.Errorf("minimax tts: HTTP %d: %s", resp.StatusCode, miniMaxErrorMessage(data))
	}
	var parsed struct {
		Data struct {
			Audio string `json:"audio"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("minimax tts: decode response: %w", err)
	}
	if parsed.BaseResp.StatusCode != 0 {
		msg := strings.TrimSpace(parsed.BaseResp.StatusMsg)
		if msg == "" {
			msg = "provider returned an error"
		}
		return nil, fmt.Errorf("minimax tts: provider status %d: %s", parsed.BaseResp.StatusCode, msg)
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

func miniMaxErrorMessage(data []byte) string {
	var payload struct {
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(data, &payload) == nil {
		if msg := strings.TrimSpace(payload.BaseResp.StatusMsg); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(payload.Error); msg != "" {
			return msg
		}
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > 2048 {
		msg = msg[:2048]
	}
	if msg == "" {
		return "provider returned an error"
	}
	return msg
}
