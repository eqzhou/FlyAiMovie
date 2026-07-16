---
name: character-scene-extractor
description: 角色和场景提取规范
---

# 角色与场景提取

## 角色字段
姓名、定位、外貌(300-500字)、性格、背景

## 场景字段
地点、时间、氛围、英文画面提示词（纯背景）

## 去重
- 角色：同名合并
- 场景：地点+时间段精确匹配

## 步骤
read_script_for_extraction → read_existing_* → save_dedup_characters / save_dedup_scenes
