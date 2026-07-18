package prompttemplate

import (
	"fmt"
	"regexp"
	"strings"
)

const MaxContentRunes = 20_000

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)

var approvedVariables = map[string]struct{}{
	"drama_title": {}, "episode_title": {}, "user_instruction": {},
	"character_names": {}, "scene_names": {},
}

var defaults = map[string]struct{ Name, Description, Content string }{
	"script_rewriter":       {"剧本改写", "将故事素材整理为短剧剧本", "你是专业短剧编剧。围绕《{{drama_title}}》的{{episode_title}}执行用户要求：{{user_instruction}}。保持人物动机连贯，输出适合分镜制作的场景与对白。"},
	"extractor":             {"角色场景提取", "从剧本整理可复用的角色与场景", "你是制片助理。分析《{{drama_title}}》的{{episode_title}}，按名称与地点语义去重角色和场景。当前要求：{{user_instruction}}。"},
	"storyboard_breaker":    {"分镜拆解", "将剧本拆分为可生成的连续镜头", "你是分镜导演。为《{{drama_title}}》的{{episode_title}}设计连续镜头，兼顾景别、机位、动作、对白和时长。当前要求：{{user_instruction}}。"},
	"voice_assigner":        {"音色分配", "依据角色特征匹配可用音色", "你是配音导演。为《{{drama_title}}》中的角色匹配音色，保持年龄、性格和情绪一致。角色：{{character_names}}。当前要求：{{user_instruction}}。"},
	"grid_prompt_generator": {"宫格提示词", "生成连贯的首帧宫格画面提示词", "你是影视画面提示词设计师。为《{{drama_title}}》的{{episode_title}}生成风格统一、角色连续的宫格画面描述。角色：{{character_names}}；场景：{{scene_names}}；当前要求：{{user_instruction}}。"},
}

type Default struct{ Key, Name, Category, Description, Content string }

func DefaultFor(key string) (Default, bool) {
	value, ok := defaults[key]
	return Default{Key: key, Name: value.Name, Category: "agent_system", Description: value.Description, Content: value.Content}, ok
}

func Defaults() []Default {
	keys := []string{"script_rewriter", "extractor", "storyboard_breaker", "voice_assigner", "grid_prompt_generator"}
	result := make([]Default, 0, len(keys))
	for _, key := range keys {
		value, _ := DefaultFor(key)
		result = append(result, value)
	}
	return result
}

func Variables(content string) ([]string, error) {
	if len([]rune(content)) > MaxContentRunes {
		return nil, fmt.Errorf("prompt content exceeds %d characters", MaxContentRunes)
	}
	matches := tokenPattern.FindAllStringSubmatch(content, -1)
	stripped := tokenPattern.ReplaceAllString(content, "")
	if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
		return nil, fmt.Errorf("malformed prompt variable")
	}
	seen := make(map[string]struct{}, len(matches))
	variables := make([]string, 0, len(matches))
	for _, match := range matches {
		name := match[1]
		if _, ok := approvedVariables[name]; !ok {
			return nil, fmt.Errorf("unknown prompt variable %q", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		variables = append(variables, name)
	}
	return variables, nil
}

func Render(content string, values map[string]string) (string, error) {
	variables, err := Variables(content)
	if err != nil {
		return "", err
	}
	for name := range values {
		if _, ok := approvedVariables[name]; !ok {
			return "", fmt.Errorf("unknown prompt variable %q", name)
		}
	}
	for _, name := range variables {
		if _, ok := values[name]; !ok {
			return "", fmt.Errorf("missing prompt variable %q", name)
		}
	}
	return tokenPattern.ReplaceAllStringFunc(content, func(token string) string {
		return values[tokenPattern.FindStringSubmatch(token)[1]]
	}), nil
}
