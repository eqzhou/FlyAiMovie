// 领域实体类型定义，镜像后端 internal/models 的 JSON 输出。
// 目的：为高频业务数据（剧集/分集/分镜等）提供类型约束，替换视图层散落的 any。
// 说明：后端时间字段统一为 string；可空外键用 number | null（对应 Go 的 *uint）。

export type EntityStatus = string

export interface Drama {
  id: number
  title: string
  description: string
  genre: string
  style: string
  total_episodes: number
  total_duration?: number
  status: EntityStatus
  thumbnail: string
  // List/detail responses decode the stored JSON tag array, while create
  // responses still serialize the underlying model field as a string.
  tags: string[] | string | null
  metadata: string
  created_at: string
  updated_at: string
  deleted_at?: string | null
  episodes?: Episode[]
  characters?: Character[]
  scenes?: Scene[]
  props?: Prop[]
}

export interface DramaListResponse {
  items: Drama[]
  pagination: {
    page: number
    page_size: number
    total: number
    total_pages: number
  }
}

export interface Episode {
  id: number
  drama_id: number
  episode_number: number
  title: string
  content: string
  script_content: string
  description: string
  duration: number
  status: EntityStatus
  video_url: string
  thumbnail: string
  image_config_id: number | null
  video_config_id: number | null
  audio_config_id: number | null
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface Character {
  id: number
  drama_id: number
  name: string
  role: string
  description: string
  appearance: string
  personality: string
  voice_style: string
  image_url: string
  reference_images: string
  seed_value: string
  sort_order: number
  local_path: string
  voice_sample_url: string
  voice_provider: string
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface CharacterTemplate {
  id: number
  name: string
  role: string
  description: string
  appearance: string
  personality: string
  voice_style: string
  voice_provider: string
  image_url: string
  reference_images: string
  local_path: string
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface Scene {
  id: number
  drama_id: number
  episode_id: number | null
  location: string
  time: string
  prompt: string
  storyboard_count: number
  image_url: string
  status: EntityStatus
  local_path: string
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface Storyboard {
  id: number
  episode_id: number
  scene_id: number | null
  storyboard_number: number
  title: string
  location: string
  time: string
  shot_type: string
  angle: string
  movement: string
  action: string
  result: string
  atmosphere: string
  image_prompt: string
  video_prompt: string
  bgm_prompt: string
  sound_effect: string
  dialogue: string
  description: string
  duration: number
  composed_image: string
  first_frame_image: string
  last_frame_image: string
  reference_images: string
  video_url: string
  tts_audio_url: string
  subtitle_url: string
  composed_video_url: string
  status: EntityStatus
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface Prop {
  id: number
  drama_id: number
  name: string
  type: string
  description: string
  prompt: string
  image_url: string
  reference_images: string
  local_path: string
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface Asset {
  id: number
  drama_id: number | null
  episode_id: number | null
  storyboard_id: number | null
  storyboard_num: number | null
  name: string
  description: string
  type: string
  category: string
  url: string
  thumbnail_url: string
  local_path: string
  file_size: number
  mime_type: string
  width: number
  height: number
  duration: number
  duration_seconds: number
  frame_rate: number
  codec: string
  probe_status: EntityStatus
  probe_error: string
  content_hash: string
  reference_count: number
  format: string
  image_gen_id: number | null
  video_gen_id: number | null
  grid_history_id: number | null
  is_favorite: boolean
  view_count: number
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface GridHistory {
  id: number
  drama_id: number | null
  episode_id: number | null
  mode: string
  split_frame_type: string
  rows: number
  cols: number
  prompt: string
  cell_prompts: string
  image_gen_id: number | null
  image_url: string
  cells_json: string
  storyboard_ids: string
  assignments_json: string
  cells_verified: boolean
  status: EntityStatus
  error_msg: string
  created_at: string
  updated_at: string
  completed_at?: string | null
}

export interface AgentConfig {
  id: number
  agent_type: string
  name: string
  description: string
  model: string
  system_prompt: string
  temperature: number | null
  max_tokens: number | null
  max_iterations: number | null
  is_active: boolean
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface PromptTemplate {
  id: number
  key: string
  name: string
  category: string
  description: string
  content: string
  variables_json: string
  version: number
  is_active: boolean
  created_at: string
  updated_at: string
  deleted_at?: string | null
}

export interface AIVoice {
  id: number
  voice_id: string
  voice_name: string
  description: string
  language: string
  provider: string
  capabilities: string
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface GenerationJob {
  id: number
  production_run_id?: number | null
  kind: string
  status: EntityStatus
  target_type: string
  target_id: number
  config_id?: number | null
  provider: string
  provider_task_id: string
  idempotency_key: string
  attempt: number
  max_attempts: number
  progress: number
  stage: string
  status_message: string
  estimated_cost: number
  actual_cost: number
  currency: string
  available_at: string
  lease_owner: string
  lease_expires_at?: string | null
  cancel_requested_at?: string | null
  started_at?: string | null
  completed_at?: string | null
  payload_json: string
  result_json: string
  last_error: string
  created_at: string
  updated_at: string
}

export interface JobEvent {
  id: number
  job_id: number
  stage: string
  progress: number
  level: string
  message: string
  cost: number
  created_at: string
}

export interface ProductionRun {
  id: number
  drama_id: number
  episode_id: number
  status: EntityStatus
  stage: string
  progress: number
  status_message: string
  last_error: string
  attempt: number
  max_attempts: number
  available_at: string
  lease_owner: string
  lease_expires_at?: string | null
  cancel_requested_at?: string | null
  started_at?: string | null
  completed_at?: string | null
  created_at: string
  updated_at: string
}

export interface AgentRun {
  id: number
  agent_type: string
  drama_id: number
  episode_id: number
  retry_of_id?: number | null
  skill_id?: number | null
  skill_version_id?: number | null
  skill_version: number
  skill_source: string
  skill_content_sha256: string
  status: EntityStatus
  input: string
  output_json: string
  last_error: string
  cancel_requested_at?: string | null
  started_at: string
  completed_at?: string | null
  created_at: string
  updated_at: string
}

export interface AgentRunEvent {
  id: number
  agent_run_id: number
  sequence: number
  event_type: string
  tool_name: string
  payload_json: string
  created_at: string
}
