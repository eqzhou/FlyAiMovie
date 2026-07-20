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
	"shot_title": {}, "shot_description": {}, "image_prompt": {}, "video_prompt": {},
	"grid_rows": {}, "grid_cols": {}, "grid_mode": {},
	"character_name": {}, "character_role": {}, "character_appearance": {}, "character_description": {}, "character_personality": {},
	"scene_location": {}, "scene_time": {}, "scene_prompt": {},
	"prop_name": {}, "prop_type": {}, "prop_description": {}, "prop_prompt": {},
}

// ApprovedVariables returns the whitelist of interpolatable prompt tokens in stable order.
func ApprovedVariables() []string {
	keys := []string{
		"drama_title", "episode_title", "user_instruction",
		"character_names", "scene_names",
		"shot_title", "shot_description", "image_prompt", "video_prompt",
		"grid_rows", "grid_cols", "grid_mode",
		"character_name", "character_role", "character_appearance", "character_description", "character_personality",
		"scene_location", "scene_time", "scene_prompt",
		"prop_name", "prop_type", "prop_description", "prop_prompt",
	}
	return append([]string(nil), keys...)
}

type Default struct{ Key, Name, Category, Description, Content string }

var defaults = map[string]Default{
	"script_rewriter":       {Name: "剧本改写", Category: "agent_system", Description: "将故事素材整理为短剧剧本", Content: "你是专业短剧编剧。围绕《{{drama_title}}》的{{episode_title}}执行用户要求：{{user_instruction}}。保持人物动机连贯，输出适合分镜制作的场景与对白。"},
	"extractor":             {Name: "角色场景提取", Category: "agent_system", Description: "从剧本整理可复用的角色与场景", Content: "你是制片助理。分析《{{drama_title}}》的{{episode_title}}，按名称与地点语义去重角色和场景。当前要求：{{user_instruction}}。"},
	"storyboard_breaker":    {Name: "分镜拆解", Category: "agent_system", Description: "将剧本拆分为可生成的连续镜头", Content: "你是分镜导演。为《{{drama_title}}》的{{episode_title}}设计连续镜头，兼顾景别、机位、动作、对白和时长。当前要求：{{user_instruction}}。"},
	"voice_assigner":        {Name: "音色分配", Category: "agent_system", Description: "依据角色特征匹配可用音色", Content: "你是配音导演。为《{{drama_title}}》中的角色匹配音色，保持年龄、性格和情绪一致。角色：{{character_names}}。当前要求：{{user_instruction}}。"},
	"grid_prompt_generator": {Name: "宫格提示词 Agent", Category: "agent_system", Description: "生成连贯的首帧宫格画面提示词", Content: "你是影视画面提示词设计师。为《{{drama_title}}》的{{episode_title}}生成风格统一、角色连续的宫格画面描述。角色：{{character_names}}；场景：{{scene_names}}；当前要求：{{user_instruction}}。"},
	"storyboard_image":      {Name: "镜头图片", Category: "image", Description: "根据镜头内容生成静态画面提示词", Content: "《{{drama_title}}》{{episode_title}}，镜头：{{shot_title}}。画面内容：{{shot_description}}。角色：{{character_names}}。场景：{{scene_names}}。电影级构图，主体清晰，视觉风格连续。"},
	"storyboard_video":      {Name: "镜头视频", Category: "video", Description: "根据镜头内容生成动态视频提示词", Content: "《{{drama_title}}》{{episode_title}}，镜头：{{shot_title}}。动作与画面：{{shot_description}}。已有画面提示：{{image_prompt}}。保持人物身份与空间连续，描述明确的运动、镜头和节奏。"},
	"grid_composition":      {Name: "宫格构图", Category: "grid", Description: "组织连续镜头的宫格画面提示词", Content: "为《{{drama_title}}》{{episode_title}}制作 {{grid_rows}}x{{grid_cols}} 宫格，模式：{{grid_mode}}。角色：{{character_names}}。场景：{{scene_names}}。制作要求：{{user_instruction}}。各格独立构图，人物与美术风格连续。"},
	"character_image":       {Name: "角色形象", Category: "image", Description: "根据角色资料生成人物立绘提示词", Content: "《{{drama_title}}》角色立绘：{{character_name}}（{{character_role}}）。外貌：{{character_appearance}}。性格：{{character_personality}}。补充：{{character_description}}。要求：{{user_instruction}}。高清正面，主体清晰，背景干净，风格统一。"},
	"scene_image":           {Name: "场景画面", Category: "image", Description: "根据场景资料生成环境画面提示词", Content: "《{{drama_title}}》场景：{{scene_location}}，时间：{{scene_time}}。画面提示：{{scene_prompt}}。要求：{{user_instruction}}。电影级空间层次，氛围明确，适合短剧镜头。"},
	"prop_image":            {Name: "道具画面", Category: "image", Description: "根据道具资料生成静物提示词", Content: "《{{drama_title}}》道具：{{prop_name}}（{{prop_type}}）。描述：{{prop_description}}。画面提示：{{prop_prompt}}。要求：{{user_instruction}}。产品摄影质感，主体居中，细节清晰。"},
}

func DefaultFor(key string) (Default, bool) {
	value, ok := defaults[key]
	value.Key = key
	return value, ok
}

func Defaults() []Default {
	keys := []string{"script_rewriter", "extractor", "storyboard_breaker", "voice_assigner", "grid_prompt_generator", "storyboard_image", "storyboard_video", "grid_composition", "character_image", "scene_image", "prop_image"}
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
