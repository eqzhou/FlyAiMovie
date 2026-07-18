package models

type MediaCacheObject struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index;uniqueIndex:idx_media_cache_object_hash" json:"organization_id"`
	ContentHash    string  `gorm:"not null;size:128;uniqueIndex:idx_media_cache_object_hash" json:"content_hash"`
	Kind           string  `gorm:"not null;size:64;index;uniqueIndex:idx_media_cache_object_hash" json:"kind"`
	LocalPath      string  `gorm:"size:1024" json:"local_path"`
	PublicURL      string  `gorm:"size:2048" json:"public_url"`
	MimeType       string  `gorm:"size:255" json:"mime_type"`
	Payload        string  `gorm:"type:text" json:"-"`
	Size           int64   `gorm:"not null;default:0" json:"size"`
	ReferenceCount int     `gorm:"not null;default:0;index" json:"reference_count"`
	Status         string  `gorm:"not null;size:32;default:active;index" json:"status"`
	ExpiresAt      *string `gorm:"index" json:"expires_at,omitempty"`
	LastAccessedAt string  `gorm:"not null;index" json:"last_accessed_at"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
}

type MediaCacheReference struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index;uniqueIndex:idx_media_cache_reference_key" json:"organization_id"`
	Namespace      string  `gorm:"not null;size:64;uniqueIndex:idx_media_cache_reference_key" json:"namespace"`
	CacheKey       string  `gorm:"not null;size:255;uniqueIndex:idx_media_cache_reference_key" json:"cache_key"`
	ObjectID       uint    `gorm:"not null;index" json:"object_id"`
	ExpiresAt      *string `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
	UpdatedAt      string  `gorm:"not null" json:"updated_at"`
}
