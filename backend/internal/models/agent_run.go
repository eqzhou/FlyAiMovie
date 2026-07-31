package models

type AgentRun struct {
	ID                 uint    `gorm:"primaryKey" json:"id"`
	OrganizationID     uint    `gorm:"not null;index:idx_agent_runs_org_created,priority:1" json:"-"`
	AgentType          string  `gorm:"not null;index" json:"agent_type"`
	DramaID            uint    `gorm:"not null;index" json:"drama_id"`
	EpisodeID          uint    `gorm:"not null;index" json:"episode_id"`
	RetryOfID          *uint   `gorm:"index" json:"retry_of_id,omitempty"`
	SkillID            *uint   `gorm:"index" json:"skill_id,omitempty"`
	SkillVersionID     *uint   `gorm:"index" json:"skill_version_id,omitempty"`
	SkillVersion       int     `json:"skill_version"`
	SkillSource        string  `json:"skill_source"`
	SkillContentSHA256 string  `gorm:"size:64" json:"skill_content_sha256"`
	SkillSnapshot      string  `gorm:"type:text" json:"skill_snapshot,omitempty"`
	Status             string  `gorm:"not null;index" json:"status"`
	Input              string  `json:"input"`
	OutputJSON         string  `json:"output_json"`
	LastError          string  `json:"last_error"`
	CancelRequestedAt  *string `json:"cancel_requested_at,omitempty"`
	StartedAt          string  `gorm:"not null" json:"started_at"`
	CompletedAt        *string `json:"completed_at,omitempty"`
	CreatedAt          string  `gorm:"not null;index:idx_agent_runs_org_created,priority:2" json:"created_at"`
	UpdatedAt          string  `gorm:"not null" json:"updated_at"`
}

type AgentRunEvent struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	OrganizationID uint   `gorm:"not null;index:idx_agent_events_org_run,priority:1" json:"-"`
	AgentRunID     uint   `gorm:"not null;index:idx_agent_events_org_run,priority:2" json:"agent_run_id"`
	Sequence       int    `gorm:"not null" json:"sequence"`
	EventType      string `gorm:"not null;index" json:"event_type"`
	ToolName       string `json:"tool_name"`
	PayloadJSON    string `json:"payload_json"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
}
