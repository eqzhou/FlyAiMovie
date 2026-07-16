---
name: storyboard-breaker
description: 分镜拆解规范
---

# 分镜拆解

每镜 10-15 秒，单一动作，字段完整：
title, shot_type, angle, movement, location, time, character_ids,
action, dialogue, description, result, atmosphere,
image_prompt, video_prompt, bgm_prompt, sound_effect, duration, scene_id

## video_prompt
按 3 秒分段，用 `<n>` 分隔，角色用 `<role>`，地点用 `<location>`。

## 步骤
read_storyboard_context → save_storyboards
