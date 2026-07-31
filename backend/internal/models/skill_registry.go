package models

// Skill is the organization-scoped identity for one of the built-in agent
// types. Content lives only in immutable SkillVersion rows.
type Skill struct {
	ID                 uint    `gorm:"primaryKey" json:"id"`
	OrganizationID     uint    `gorm:"not null;uniqueIndex:idx_skills_org_agent,priority:1" json:"-"`
	AgentType          string  `gorm:"not null;uniqueIndex:idx_skills_org_agent,priority:2" json:"agent_type"`
	PublishedVersionID *uint   `gorm:"index" json:"published_version_id,omitempty"`
	ArchivedAt         *string `gorm:"index" json:"archived_at,omitempty"`
	CreatedAt          string  `gorm:"not null" json:"created_at"`
	UpdatedAt          string  `gorm:"not null" json:"updated_at"`
}

type SkillVersion struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	OrganizationID  uint   `gorm:"not null;index" json:"-"`
	SkillID         uint   `gorm:"not null;uniqueIndex:idx_skill_versions_number,priority:1" json:"skill_id"`
	Version         int    `gorm:"not null;uniqueIndex:idx_skill_versions_number,priority:2" json:"version"`
	MainMarkdown    string `gorm:"type:text;not null" json:"main_markdown"`
	ReferencesJSON  string `gorm:"type:text;not null" json:"references_json"`
	ContentSHA256   string `gorm:"size:64;not null;index" json:"content_sha256"`
	CreatedByUserID uint   `gorm:"not null" json:"created_by_user_id"`
	CreatedAt       string `gorm:"not null" json:"created_at"`
}

type SkillPublication struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	OrganizationID  uint   `gorm:"not null;index:idx_skill_publications_org_skill,priority:1" json:"-"`
	SkillID         uint   `gorm:"not null;index:idx_skill_publications_org_skill,priority:2" json:"skill_id"`
	VersionID       *uint  `gorm:"index" json:"version_id,omitempty"`
	Action          string `gorm:"not null;index" json:"action"`
	CreatedByUserID uint   `gorm:"not null" json:"created_by_user_id"`
	CreatedAt       string `gorm:"not null" json:"created_at"`
}
