---
name: storyboard-breaker
description: 分镜拆解规范
---

# 分镜拆解

## 镜头粒度
- 每镜约 10-15 秒，单一主动作或单一信息点
- 一场多镜；景别变化服务情绪与信息
- 避免一镜塞满整场对话

## 必填字段
title, shot_type, angle, movement, location, time, character_ids,
action, dialogue, description, result, atmosphere,
image_prompt, video_prompt, bgm_prompt, sound_effect, duration, scene_id

## 字段要点
- shot_type：特写/近景/中景/全景/远景等
- angle：平视/仰拍/俯拍/侧拍
- movement：固定/推/拉/摇/移/跟
- action：可见动作，现在时
- dialogue：本镜实际台词，无则空
- image_prompt：英文首帧/构图描述，含人物外观线索与环境
- video_prompt：按约 3 秒分段，用 `<n>` 分隔；角色用 `<role>`，地点用 `<location>`
- duration：秒，整数或合理小数

## 步骤
`read_storyboard_context` → 规划镜头序列 → `save_storyboards`

## 质量检查
- 镜头顺序覆盖剧本关键节拍
- character_ids / scene_id 与已有资产对齐
- image_prompt 与 video_prompt 不互相矛盾
