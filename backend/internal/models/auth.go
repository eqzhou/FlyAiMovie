package models

type Organization struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"not null" json:"name"`
	Slug      string `gorm:"uniqueIndex;not null" json:"slug"`
	Status    string `gorm:"not null;default:active" json:"status"`
	CreatedAt string `gorm:"not null" json:"created_at"`
	UpdatedAt string `gorm:"not null" json:"updated_at"`
}

type User struct {
	ID              uint    `gorm:"primaryKey" json:"id"`
	Email           string  `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash    string  `gorm:"not null" json:"-"`
	DisplayName     string  `gorm:"not null" json:"display_name"`
	Status          string  `gorm:"not null;default:active" json:"status"`
	IsPlatformAdmin bool    `gorm:"not null;default:false" json:"is_platform_admin"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
	LastLoginAt     *string `json:"last_login_at,omitempty"`
	CreatedAt       string  `gorm:"not null" json:"created_at"`
	UpdatedAt       string  `gorm:"not null" json:"updated_at"`
}

type PlatformSettings struct {
	ID                       uint   `gorm:"primaryKey" json:"id"`
	RegistrationEnabled      bool   `gorm:"not null;default:true" json:"registration_enabled"`
	RequireEmailVerification bool   `gorm:"not null;default:false" json:"require_email_verification"`
	UpdatedAt                string `gorm:"not null" json:"updated_at"`
	UpdatedBy                *uint  `json:"updated_by,omitempty"`
}

type Membership struct {
	OrganizationID uint   `gorm:"primaryKey;index" json:"organization_id"`
	UserID         uint   `gorm:"primaryKey;index" json:"user_id"`
	Role           string `gorm:"not null" json:"role"`
	CreatedAt      string `gorm:"not null" json:"created_at"`
	UpdatedAt      string `gorm:"not null" json:"updated_at"`
}

type OrganizationInvitation struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index" json:"organization_id"`
	InvitedBy      uint    `gorm:"not null" json:"invited_by"`
	Email          string  `gorm:"not null;index" json:"email"`
	Role           string  `gorm:"not null" json:"role"`
	TokenHash      string  `gorm:"uniqueIndex;not null;size:64" json:"-"`
	ExpiresAt      string  `gorm:"not null;index" json:"expires_at"`
	AcceptedAt     *string `json:"accepted_at,omitempty"`
	RevokedAt      *string `json:"revoked_at,omitempty"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
}

type PasswordResetToken struct {
	ID         uint    `gorm:"primaryKey" json:"id"`
	UserID     uint    `gorm:"not null;index" json:"user_id"`
	TokenHash  string  `gorm:"uniqueIndex;not null;size:64" json:"-"`
	ExpiresAt  string  `gorm:"not null;index" json:"expires_at"`
	ConsumedAt *string `json:"consumed_at,omitempty"`
	CreatedAt  string  `gorm:"not null" json:"created_at"`
}

type Session struct {
	TokenHash      string  `gorm:"primaryKey;size:64" json:"-"`
	CSRFToken      string  `gorm:"not null;size:64" json:"-"`
	UserID         uint    `gorm:"not null;index" json:"user_id"`
	OrganizationID uint    `gorm:"not null;index" json:"organization_id"`
	ExpiresAt      string  `gorm:"not null;index" json:"expires_at"`
	LastSeenAt     string  `gorm:"not null" json:"last_seen_at"`
	RevokedAt      *string `json:"revoked_at,omitempty"`
	CreatedAt      string  `gorm:"not null" json:"created_at"`
}

type AuditLog struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	OrganizationID uint   `gorm:"not null;index:idx_audit_org_created,priority:1" json:"-"`
	UserID         uint   `gorm:"not null;index" json:"user_id"`
	Role           string `gorm:"not null" json:"role"`
	Action         string `gorm:"not null;index" json:"action"`
	ResourceType   string `gorm:"not null;index" json:"resource_type"`
	ResourceID     string `json:"resource_id"`
	Method         string `gorm:"not null" json:"method"`
	Path           string `gorm:"not null" json:"path"`
	StatusCode     int    `gorm:"not null" json:"status_code"`
	SourceIP       string `json:"source_ip"`
	CreatedAt      string `gorm:"not null;index:idx_audit_org_created,priority:2" json:"created_at"`
}

type OrganizationQuota struct {
	OrganizationID       uint    `gorm:"primaryKey" json:"organization_id"`
	DailyJobLimit        int     `gorm:"not null;default:200" json:"daily_job_limit"`
	MaxActiveJobs        int     `gorm:"not null;default:10" json:"max_active_jobs"`
	DailyBudgetCNY       float64 `gorm:"not null;default:0" json:"daily_budget_cny"`
	BudgetWarningPercent int     `gorm:"not null;default:80" json:"budget_warning_percent"`
	CreatedAt            string  `gorm:"not null" json:"created_at"`
	UpdatedAt            string  `gorm:"not null" json:"updated_at"`
}
