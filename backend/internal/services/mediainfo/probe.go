package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	Duration  float64
	Width     int
	Height    int
	FrameRate float64
	Codec     string
	Format    string
}

type probeResponse struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

func Probe(ctx context.Context, path string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	return Parse(output)
}

func Parse(data []byte) (Info, error) {
	var response probeResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return Info{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	info := Info{Format: response.Format.FormatName}
	info.Duration, _ = strconv.ParseFloat(response.Format.Duration, 64)
	for _, stream := range response.Streams {
		if stream.CodecType == "video" && info.Width == 0 {
			info.Width, info.Height, info.Codec = stream.Width, stream.Height, stream.CodecName
			info.FrameRate = parseRate(stream.AvgFrameRate)
		}
		if stream.CodecType == "audio" && info.Codec == "" {
			info.Codec = stream.CodecName
		}
	}
	if info.Duration <= 0 && info.Codec == "" {
		return Info{}, fmt.Errorf("ffprobe returned no usable media metadata")
	}
	return info, nil
}

func parseRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		numerator, _ := strconv.ParseFloat(parts[0], 64)
		denominator, _ := strconv.ParseFloat(parts[1], 64)
		if denominator != 0 {
			return numerator / denominator
		}
	}
	rate, _ := strconv.ParseFloat(value, 64)
	return rate
}
