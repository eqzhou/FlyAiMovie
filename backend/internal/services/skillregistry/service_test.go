package skillregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Organization{}, &models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}); err != nil {
		t.Fatal(err)
	}
	organizations := make([]models.Organization, 0, 20)
	for id := uint(1); id <= 20; id++ {
		organizations = append(organizations, models.Organization{ID: id, Name: "Org", Slug: fmt.Sprintf("org-%d", id), Status: "active", CreatedAt: "now", UpdatedAt: "now"})
	}
	if err := database.Create(&organizations).Error; err != nil {
		t.Fatal(err)
	}
	return database
}

func TestMutationRejectsOrganizationThatNoLongerExists(t *testing.T) {
	service := New(testDB(t))
	if _, err := service.CreateVersion(99, 1, "extractor", VersionInput{MainMarkdown: "orphan"}); err == nil {
		t.Fatal("expected missing organization to reject skill creation")
	}
}

func TestCreateVersionValidatesAndHashesImmutableContent(t *testing.T) {
	service := New(testDB(t))
	created, err := service.CreateVersion(7, 11, "script_rewriter", VersionInput{
		MainMarkdown: "# Rewrite\nKeep intent.",
		References:   map[string]string{"references/style.md": "concise"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || len(created.ContentSHA256) != 64 {
		t.Fatalf("version=%d hash=%q", created.Version, created.ContentSHA256)
	}
	rendered, err := RenderVersion(*created)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(rendered))
	if created.ContentSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("hash=%q want snapshot hash=%q snapshot=%q", created.ContentSHA256, hex.EncodeToString(digest[:]), rendered)
	}
	second, err := service.CreateVersion(7, 11, "script_rewriter", VersionInput{MainMarkdown: "# Rewrite\nNew"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || second.ContentSHA256 == created.ContentSHA256 {
		t.Fatalf("second=%+v", second)
	}
	var first models.SkillVersion
	if err := service.DB().First(&first, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.MainMarkdown != "# Rewrite\nKeep intent." {
		t.Fatalf("first version mutated: %q", first.MainMarkdown)
	}
}

func TestLocalWorkspacePrincipalIsAcceptedAndMixedPrincipalsAreRejected(t *testing.T) {
	service := New(testDB(t))
	created, err := service.CreateVersion(0, 0, "extractor", VersionInput{MainMarkdown: "local skill"})
	if err != nil {
		t.Fatalf("create local version: %v", err)
	}
	if _, err := service.Publish(0, 0, "extractor", created.ID); err != nil {
		t.Fatalf("publish local version: %v", err)
	}
	detail, err := service.Get(0, "extractor")
	if err != nil || detail.PublishedVersion == nil || detail.PublishedVersion.ID != created.ID {
		t.Fatalf("local detail=%+v err=%v", detail, err)
	}
	resolved, err := service.ResolvePublished(0, "extractor")
	if err != nil || resolved.ID != created.ID {
		t.Fatalf("local resolved=%+v err=%v", resolved, err)
	}

	for _, principal := range []struct{ organizationID, userID uint }{{0, 9}, {7, 0}} {
		if _, err := service.CreateVersion(principal.organizationID, principal.userID, "extractor", VersionInput{MainMarkdown: "mixed"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mixed principal (%d,%d) err=%v", principal.organizationID, principal.userID, err)
		}
		if _, err := service.Publish(principal.organizationID, principal.userID, "extractor", created.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mixed publish principal (%d,%d) err=%v", principal.organizationID, principal.userID, err)
		}
		if _, err := service.Archive(principal.organizationID, principal.userID, "extractor"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mixed archive principal (%d,%d) err=%v", principal.organizationID, principal.userID, err)
		}
	}
}

func TestPublicationRollbackAndOrganizationIsolation(t *testing.T) {
	service := New(testDB(t))
	v1, _ := service.CreateVersion(1, 10, "extractor", VersionInput{MainMarkdown: "v1"})
	v2, _ := service.CreateVersion(1, 10, "extractor", VersionInput{MainMarkdown: "v2"})
	if _, err := service.Publish(2, 10, "extractor", v1.ID); err != ErrNotFound {
		t.Fatalf("cross-org publish err=%v", err)
	}
	if _, err := service.Publish(1, 10, "extractor", v2.ID); err != nil {
		t.Fatal(err)
	}
	rolled, err := service.Rollback(1, 10, "extractor", v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolled.PublishedVersionID == nil || *rolled.PublishedVersionID != v1.ID {
		t.Fatalf("published=%v", rolled.PublishedVersionID)
	}
	detail, err := service.Get(1, "extractor")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Publications) != 2 || detail.PublishedVersion == nil || detail.PublishedVersion.MainMarkdown != "v1" {
		t.Fatalf("detail=%+v", detail)
	}
}

func TestValidationRejectsUnknownAgentUnsafePathsAndOversize(t *testing.T) {
	service := New(testDB(t))
	cases := []struct {
		name  string
		agent string
		input VersionInput
	}{
		{"unknown agent", "new_agent", VersionInput{MainMarkdown: "ok"}},
		{"empty main", "extractor", VersionInput{}},
		{"traversal", "extractor", VersionInput{MainMarkdown: "ok", References: map[string]string{"../secret.md": "x"}}},
		{"absolute", "extractor", VersionInput{MainMarkdown: "ok", References: map[string]string{"/tmp/x.md": "x"}}},
		{"wrong root", "extractor", VersionInput{MainMarkdown: "ok", References: map[string]string{"other/x.md": "x"}}},
		{"oversize", "extractor", VersionInput{MainMarkdown: strings.Repeat("x", MaxMainBytes+1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.CreateVersion(1, 1, tc.agent, tc.input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestResolvePublishedAndArchive(t *testing.T) {
	service := New(testDB(t))
	v, _ := service.CreateVersion(4, 9, "voice_assigner", VersionInput{MainMarkdown: "db skill"})
	if _, err := service.ResolvePublished(4, "voice_assigner"); err != ErrNotFound {
		t.Fatalf("unpublished err=%v", err)
	}
	if _, err := service.Publish(4, 9, "voice_assigner", v.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := service.ResolvePublished(4, "voice_assigner")
	if err != nil || resolved.ID != v.ID {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if _, err := service.Archive(4, 9, "voice_assigner"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolvePublished(4, "voice_assigner"); err != ErrNotFound {
		t.Fatalf("archived err=%v", err)
	}
	restored, err := service.Publish(4, 9, "voice_assigner", v.ID)
	if err != nil {
		t.Fatalf("republish archived version: %v", err)
	}
	if restored.ArchivedAt != nil || restored.PublishedVersionID == nil || *restored.PublishedVersionID != v.ID {
		t.Fatalf("restored=%+v", restored)
	}
	if resolved, err := service.ResolvePublished(4, "voice_assigner"); err != nil || resolved.ID != v.ID {
		t.Fatalf("resolved restored=%+v err=%v", resolved, err)
	}
}

func TestResolvePublishedPreservesDatabaseErrors(t *testing.T) {
	database := testDB(t)
	service := New(database)
	version, err := service.CreateVersion(5, 9, "extractor", VersionInput{MainMarkdown: "published"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(5, 9, "extractor", version.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrator().DropTable(&models.SkillVersion{}); err != nil {
		t.Fatal(err)
	}
	_, err = service.ResolvePublished(5, "extractor")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("expected database error, got %v", err)
	}
}
