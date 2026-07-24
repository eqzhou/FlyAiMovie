---
name: grid-image-generator
description: 角色/场景/宫格英文提示词
---

# 图片提示词

## 通用
- 英文提示词；强调 cinematic quality、consistent art style
- 禁止字幕、水印、logo、文字面板
- 保持同一项目内人物与场景风格统一

## 角色
appearance + temperament + cinematic portrait
- 半身或全身按用途选择
- 突出识别特征（发型、服装、标志物）

## 场景
location + lighting + atmosphere + cinematic scene
- 纯环境，不出现可识别主角面部特写抢戏

## 宫格
- exactly N panels, no merged/missing panels
- 面板之间构图独立但风格一致
- 模式：first_frame / first_last / multi_ref

## 步骤
读取镜头上下文 → 生成对应模式提示词 → 通过工具写回
