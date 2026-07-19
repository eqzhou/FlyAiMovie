package models

type GenerationJob struct {
	OrganizationID    uint    `gorm:"not null;default:0;index" json:"-"`
	ID                uint    `gorm:"primaryKey" json:"id"`
	ProductionRunID   *uint   `gorm:"index" json:"production_run_id,omitempty"`
	Kind              string  `gorm:"not null;index" json:"kind"`
	Status            string  `gorm:"not null;index:idx_jobs_ready" json:"status"`
	TargetType        string  `gorm:"not null;index:idx_job_target" json:"target_type"`
	TargetID          uint    `gorm:"not null;index:idx_job_target" json:"target_id"`
	ConfigID          *uint   `json:"config_id,omitempty"`
	Provider          string  `json:"provider"`
	ProviderTaskID    string  `gorm:"index" json:"provider_task_id"`
	IdempotencyKey    string  `gorm:"index" json:"idempotency_key"`
	Attempt           int     `gorm:"not null;default:1" json:"attempt"`
	MaxAttempts       int     `gorm:"not null;default:3" json:"max_attempts"`
	Progress          int     `gorm:"not null;default:0" json:"progress"`
	Stage             string  `json:"stage"`
	StatusMessage     string  `json:"status_message"`
	EstimatedCost     float64 `gorm:"not null;default:0" json:"estimated_cost"`
	ActualCost        float64 `gorm:"not null;default:0" json:"actual_cost"`
	Currency          string  `gorm:"not null;default:CNY" json:"currency"`
	AvailableAt       string  `gorm:"not null;index:idx_jobs_ready" json:"available_at"`
	LeaseOwner        string  `json:"lease_owner"`
	LeaseExpiresAt    *string `json:"lease_expires_at,omitempty"`
	CancelRequestedAt *string `json:"cancel_requested_at,omitempty"`
	StartedAt         *string `json:"started_at,omitempty"`
	CompletedAt       *string `json:"completed_at,omitempty"`
	PayloadJSON       string  `json:"payload_json"`
	ResultJSON        string  `json:"result_json"`
	LastError         string  `json:"last_error"`
	CreatedAt         string  `gorm:"not null" json:"created_at"`
	UpdatedAt         string  `gorm:"not null" json:"updated_at"`
}

// JobEvent is an append-only operational history for a generation job.
type JobEvent struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index:idx_job_events_org_job,priority:1" json:"-"`
	JobID          uint    `gorm:"not null;index:idx_job_events_org_job,priority:2" json:"job_id"`
	Stage          string  `gorm:"not null;index" json:"stage"`
	Progress       int     `gorm:"not null;default:0" json:"progress"`
	Level          string  `gorm:"not null;default:info" json:"level"`
	Message        string  `json:"message"`
	Cost           float64 `gorm:"not null;default:0" json:"cost"`
	CreatedAt      string  `gorm:"not null;index" json:"created_at"`
}
