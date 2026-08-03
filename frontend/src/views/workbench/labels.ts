// 工作台纯展示辅助函数：状态/阶段中文标签映射与参考图归一化。
// 这些函数不依赖组件响应式状态，独立成模块以便复用与单测。

const GRID_FRAME_LABELS: Record<string, string> = {
  first_frame: '首帧',
  last_frame: '尾帧',
  composed: '分镜板',
}

const PRODUCTION_STAGE_LABELS: Record<string, string> = {
  script: '剧本生成',
  extract: '角色场景提取',
  storyboards: '分镜拆解',
  frames: '首帧生成',
  videos: '视频生成',
  tts: '对白配音',
  compose: '镜头合成',
  merge: '成片导出',
  completed: '制作完成',
}

const PRODUCTION_STATUS_LABELS: Record<string, string> = {
  queued: '制作中',
  succeeded: '已完成',
  failed: '失败',
  canceled: '已取消',
}

const STORYBOARD_STATUS_LABELS: Record<string, string> = {
  pending: '待制作',
  processing: '制作中',
  generated: '已生成',
  composed: '已合成',
  completed: '已完成',
  failed: '失败',
}

export function gridFrameLabel(frameType: string): string {
  return GRID_FRAME_LABELS[frameType] || frameType
}

export function productionStageLabel(stage?: string): string {
  return PRODUCTION_STAGE_LABELS[stage || ''] || '准备中'
}

export function productionStatusLabel(status?: string): string {
  return PRODUCTION_STATUS_LABELS[status || ''] || status || '等待中'
}

export function storyboardStatusLabel(value?: string): string {
  return STORYBOARD_STATUS_LABELS[value || ''] || value || '待制作'
}

// 镜头状态点：优先级 已合成 > 有视频 > 失败 > 无。
export function shotStatusDot(sb: { composed_video_url?: string; video_url?: string; status?: string }): string {
  if (sb.composed_video_url) return 'ok'
  if (sb.video_url) return 'run'
  if (sb.status === 'failed') return 'fail'
  return ''
}

// 参考图字段可能是 JSON 数组字符串或换行分隔文本，统一归一化为按行文本。
export function referenceImagesToText(value: unknown): string {
  const raw = typeof value === 'string' ? value.trim() : ''
  if (!raw) return ''
  if (raw.startsWith('[')) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        return parsed.map((item) => String(item ?? '').trim()).filter(Boolean).join('\n')
      }
    } catch {
      // 非法 JSON 时退回按行解析
    }
  }
  return raw.split(/\r?\n/).map((item) => item.trim()).filter(Boolean).join('\n')
}
