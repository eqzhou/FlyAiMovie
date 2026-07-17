package models

type MediaDeletionTask struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index;uniqueIndex:idx_media_delete_path" json:"organization_id"`
	PathHash       string  `gorm:"not null;size:64;uniqueIndex:idx_media_delete_path" json:"-"`
	LocalPath      string  `gorm:"not null" json:"local_path"`
	Reason         string  `json:"reason"`
	Status         string  `gorm:"not null;default:pending;index" json:"status"`
	Attempts       int     `gorm:"not null;default:0" json:"attempts"`
	AvailableAt    string  `gorm:"not null;index" json:"available_at"`
	LastError      string  `json:"last_error"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
	CompletedAt    *string `json:"completed_at,omitempty"`
}
