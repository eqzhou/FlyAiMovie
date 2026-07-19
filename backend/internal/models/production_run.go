package models

// ProductionRun is the durable parent state for one complete episode build.
type ProductionRun struct {
	OrganizationID    uint    `gorm:"not null;index:idx_production_org_episode,priority:1" json:"-"`
	ID                uint    `gorm:"primaryKey" json:"id"`
	DramaID           uint    `gorm:"not null;index" json:"drama_id"`
	EpisodeID         uint    `gorm:"not null;index:idx_production_org_episode,priority:2" json:"episode_id"`
	Status            string  `gorm:"not null;index:idx_production_ready" json:"status"`
	Stage             string  `gorm:"not null" json:"stage"`
	Progress          int     `gorm:"not null;default:0" json:"progress"`
	StatusMessage     string  `json:"status_message"`
	LastError         string  `json:"last_error"`
	Attempt           int     `gorm:"not null;default:1" json:"attempt"`
	MaxAttempts       int     `gorm:"not null;default:3" json:"max_attempts"`
	AvailableAt       string  `gorm:"not null;index:idx_production_ready" json:"available_at"`
	LeaseOwner        string  `json:"lease_owner"`
	LeaseExpiresAt    *string `json:"lease_expires_at,omitempty"`
	CancelRequestedAt *string `json:"cancel_requested_at,omitempty"`
	StartedAt         *string `json:"started_at,omitempty"`
	CompletedAt       *string `json:"completed_at,omitempty"`
	CreatedAt         string  `gorm:"not null" json:"created_at"`
	UpdatedAt         string  `gorm:"not null" json:"updated_at"`
}
