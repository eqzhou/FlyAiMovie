package agents

import (
	"regexp"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/textutil"
)

// ignoreSpeaker used by offline extractor; reuse generation-like heuristics
var ignoreSpeaker = regexp.MustCompile(`^(环境音|环境声|音效|效果音|sfx|sound ?effect|bgm|背景音|背景音乐|ambient|旁白|OS|VO)$`)

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// try extract first { ... }
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func asString(v any) string {
	return textutil.AsString(v, true)
}

func asInt(v any) int {
	return textutil.AsInt(v, true)
}

func asUint(v any) uint {
	return textutil.AsUint(v, true)
}
