package skillregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxMainBytes       = 256 * 1024
	MaxReferencesBytes = 512 * 1024
	MaxReferenceFiles  = 64
)

var (
	ErrNotFound = errors.New("skill not found")
	ErrInvalid  = errors.New("invalid skill input")
)

var validAgents = map[string]bool{
	"script_rewriter": true, "extractor": true, "storyboard_breaker": true,
	"voice_assigner": true, "grid_prompt_generator": true,
}

type VersionInput struct {
	MainMarkdown string            `json:"main_markdown"`
	References   map[string]string `json:"references"`
}

type Detail struct {
	models.Skill
	Versions         []models.SkillVersion     `json:"versions"`
	Publications     []models.SkillPublication `json:"publications"`
	PublishedVersion *models.SkillVersion      `json:"published_version,omitempty"`
}

type Service struct{ database *gorm.DB }

func New(database *gorm.DB) *Service { return &Service{database: database} }
func (s *Service) DB() *gorm.DB      { return s.database }

func IsValidAgent(agentType string) bool { return validAgents[agentType] }

func validPrincipal(organizationID, userID uint) bool {
	return (organizationID == 0 && userID == 0) || (organizationID > 0 && userID > 0)
}

// RenderVersion builds the exact deterministic text appended to an agent's
// system prompt. References are ordered by their canonical path.
func RenderVersion(version models.SkillVersion) (string, error) {
	var references map[string]string
	if err := json.Unmarshal([]byte(version.ReferencesJSON), &references); err != nil {
		return "", fmt.Errorf("decode skill references: %w", err)
	}
	paths := make([]string, 0, len(references))
	for name := range references {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	sections := []string{strings.TrimSpace(version.MainMarkdown)}
	for _, name := range paths {
		if body := strings.TrimSpace(references[name]); body != "" {
			sections = append(sections, body)
		}
	}
	return strings.Join(sections, "\n\n"), nil
}

func (s *Service) List(organizationID uint, includeArchived bool) ([]models.Skill, error) {
	query := s.database.Where("organization_id = ?", organizationID)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var rows []models.Skill
	return rows, query.Order("agent_type").Find(&rows).Error
}

func (s *Service) Get(organizationID uint, agentType string) (*Detail, error) {
	root, err := s.findRoot(s.database, organizationID, agentType, true)
	if err != nil {
		return nil, err
	}
	detail := &Detail{Skill: *root, Versions: []models.SkillVersion{}, Publications: []models.SkillPublication{}}
	if err := s.database.Where("organization_id = ? AND skill_id = ?", organizationID, root.ID).Order("version DESC").Find(&detail.Versions).Error; err != nil {
		return nil, err
	}
	if err := s.database.Where("organization_id = ? AND skill_id = ?", organizationID, root.ID).Order("id DESC").Find(&detail.Publications).Error; err != nil {
		return nil, err
	}
	if root.PublishedVersionID != nil {
		var published models.SkillVersion
		if err := s.database.Where("organization_id = ? AND skill_id = ? AND id = ?", organizationID, root.ID, *root.PublishedVersionID).First(&published).Error; err != nil {
			return nil, err
		}
		detail.PublishedVersion = &published
	}
	return detail, nil
}

func (s *Service) CreateVersion(organizationID, userID uint, agentType string, input VersionInput) (*models.SkillVersion, error) {
	refsJSON, digest, err := validateVersionInput(organizationID, userID, agentType, input)
	if err != nil {
		return nil, err
	}
	var created models.SkillVersion
	err = s.database.Transaction(func(tx *gorm.DB) error {
		if err := lockOrganization(tx, organizationID); err != nil {
			return err
		}
		root, err := s.findRoot(tx.Clauses(clause.Locking{Strength: "UPDATE"}), organizationID, agentType, true)
		if err == ErrNotFound {
			now := response.Now()
			root = &models.Skill{OrganizationID: organizationID, AgentType: agentType, CreatedAt: now, UpdatedAt: now}
			if createErr := tx.Create(root).Error; createErr != nil {
				return createErr
			}
		} else if err != nil {
			return err
		}
		if root.ArchivedAt != nil {
			return fmt.Errorf("%w: skill is archived", ErrInvalid)
		}
		var latest int
		if err := tx.Model(&models.SkillVersion{}).Where("organization_id = ? AND skill_id = ?", organizationID, root.ID).Select("COALESCE(MAX(version), 0)").Scan(&latest).Error; err != nil {
			return err
		}
		created = models.SkillVersion{OrganizationID: organizationID, SkillID: root.ID, Version: latest + 1, MainMarkdown: input.MainMarkdown, ReferencesJSON: refsJSON, ContentSHA256: digest, CreatedByUserID: userID, CreatedAt: response.Now()}
		return tx.Create(&created).Error
	})
	return &created, err
}

func (s *Service) Publish(organizationID, userID uint, agentType string, versionID uint) (*models.Skill, error) {
	return s.setPublished(organizationID, userID, agentType, versionID, "publish")
}

func (s *Service) Rollback(organizationID, userID uint, agentType string, versionID uint) (*models.Skill, error) {
	return s.setPublished(organizationID, userID, agentType, versionID, "rollback")
}

func (s *Service) setPublished(organizationID, userID uint, agentType string, versionID uint, action string) (*models.Skill, error) {
	if !validPrincipal(organizationID, userID) || versionID == 0 || !IsValidAgent(agentType) {
		return nil, ErrInvalid
	}
	var root models.Skill
	err := s.database.Transaction(func(tx *gorm.DB) error {
		if err := lockOrganization(tx, organizationID); err != nil {
			return err
		}
		// Publishing an existing version is also the recovery path for an
		// archived skill. The update below clears archived_at atomically.
		found, err := s.findRoot(tx.Clauses(clause.Locking{Strength: "UPDATE"}), organizationID, agentType, true)
		if err != nil {
			return err
		}
		root = *found
		var version models.SkillVersion
		if err := tx.Where("organization_id = ? AND skill_id = ? AND id = ?", organizationID, root.ID, versionID).First(&version).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		now := response.Now()
		if err := tx.Model(&root).Updates(map[string]any{"published_version_id": version.ID, "archived_at": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		root.PublishedVersionID, root.ArchivedAt, root.UpdatedAt = &version.ID, nil, now
		return tx.Create(&models.SkillPublication{OrganizationID: organizationID, SkillID: root.ID, VersionID: &version.ID, Action: action, CreatedByUserID: userID, CreatedAt: now}).Error
	})
	return &root, err
}

func (s *Service) Archive(organizationID, userID uint, agentType string) (*models.Skill, error) {
	if !validPrincipal(organizationID, userID) || !IsValidAgent(agentType) {
		return nil, ErrInvalid
	}
	var root models.Skill
	err := s.database.Transaction(func(tx *gorm.DB) error {
		if err := lockOrganization(tx, organizationID); err != nil {
			return err
		}
		found, err := s.findRoot(tx.Clauses(clause.Locking{Strength: "UPDATE"}), organizationID, agentType, false)
		if err != nil {
			return err
		}
		root = *found
		now := response.Now()
		if err := tx.Model(&root).Updates(map[string]any{"published_version_id": nil, "archived_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		root.PublishedVersionID, root.ArchivedAt, root.UpdatedAt = nil, &now, now
		return tx.Create(&models.SkillPublication{OrganizationID: organizationID, SkillID: root.ID, Action: "archive", CreatedByUserID: userID, CreatedAt: now}).Error
	})
	return &root, err
}

func lockOrganization(tx *gorm.DB, organizationID uint) error {
	if organizationID == 0 {
		return nil
	}
	var organization models.Organization
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", organizationID).First(&organization).Error
}

func (s *Service) ResolvePublished(organizationID uint, agentType string) (*models.SkillVersion, error) {
	root, err := s.findRoot(s.database, organizationID, agentType, false)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if root.ArchivedAt != nil || root.PublishedVersionID == nil {
		return nil, ErrNotFound
	}
	var version models.SkillVersion
	if err := s.database.Where("organization_id = ? AND skill_id = ? AND id = ?", organizationID, root.ID, *root.PublishedVersionID).First(&version).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &version, nil
}

func (s *Service) findRoot(query *gorm.DB, organizationID uint, agentType string, includeArchived bool) (*models.Skill, error) {
	if !IsValidAgent(agentType) {
		return nil, ErrInvalid
	}
	query = query.Where("organization_id = ? AND agent_type = ?", organizationID, agentType)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	var root models.Skill
	if err := query.First(&root).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &root, nil
}

func validateVersionInput(organizationID, userID uint, agentType string, input VersionInput) (string, string, error) {
	if !validPrincipal(organizationID, userID) || !IsValidAgent(agentType) {
		return "", "", ErrInvalid
	}
	if strings.TrimSpace(input.MainMarkdown) == "" || len([]byte(input.MainMarkdown)) > MaxMainBytes {
		return "", "", fmt.Errorf("%w: invalid main markdown size", ErrInvalid)
	}
	if len(input.References) > MaxReferenceFiles {
		return "", "", fmt.Errorf("%w: too many references", ErrInvalid)
	}
	total := 0
	for name, content := range input.References {
		clean := path.Clean(name)
		if clean != name || strings.HasPrefix(name, "/") || !strings.HasPrefix(name, "references/") || clean == "references" || strings.Contains(name, "\\") {
			return "", "", fmt.Errorf("%w: unsafe reference path", ErrInvalid)
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") || len(name) > 240 {
			return "", "", fmt.Errorf("%w: invalid reference path", ErrInvalid)
		}
		total += len(name) + len([]byte(content))
		if total > MaxReferencesBytes {
			return "", "", fmt.Errorf("%w: references too large", ErrInvalid)
		}
	}
	encoded, err := json.Marshal(input.References)
	if err != nil {
		return "", "", fmt.Errorf("%w: references", ErrInvalid)
	}
	if input.References == nil {
		encoded = []byte("{}")
	}
	snapshot, err := RenderVersion(models.SkillVersion{MainMarkdown: input.MainMarkdown, ReferencesJSON: string(encoded)})
	if err != nil {
		return "", "", fmt.Errorf("%w: render version: %v", ErrInvalid, err)
	}
	digest := sha256.Sum256([]byte(snapshot))
	return string(encoded), hex.EncodeToString(digest[:]), nil
}
