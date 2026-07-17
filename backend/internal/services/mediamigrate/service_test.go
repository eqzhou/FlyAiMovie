package mediamigrate

import (
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestIsRemote(t *testing.T) {
	if !IsRemote("https://cdn.example/a.png") {
		t.Fatal("expected remote URL")
	}
	for _, value := range []string{"/static/a.png", "file:///tmp/a.png", "", "not a url"} {
		if IsRemote(value) {
			t.Fatalf("unexpected remote %q", value)
		}
	}
}

func TestReplaceMediaReferencesIsOrganizationScoped(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/migration.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	oldURL := "https://cdn.example/old.png"
	owned := models.Character{OrganizationID: 7, DramaID: 1, Name: "owned", ImageURL: oldURL, CreatedAt: now, UpdatedAt: now}
	other := models.Character{OrganizationID: 8, DramaID: 2, Name: "other", ImageURL: oldURL, CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: 7, EpisodeID: 1, StoryboardNumber: 1, FirstFrameImage: oldURL, LastFrameImage: oldURL, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&owned).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	if err := replaceMediaReferences(database, Candidate{OrganizationID: 7, Kind: "image", SourceURL: oldURL}, "/static/images/local.png"); err != nil {
		t.Fatal(err)
	}
	database.First(&owned, owned.ID)
	database.First(&other, other.ID)
	database.First(&storyboard, storyboard.ID)
	if owned.ImageURL != "/static/images/local.png" || storyboard.FirstFrameImage != "/static/images/local.png" || storyboard.LastFrameImage != "/static/images/local.png" {
		t.Fatalf("owned references not replaced: owned=%+v storyboard=%+v", owned, storyboard)
	}
	if other.ImageURL != oldURL {
		t.Fatalf("cross-organization reference changed: %+v", other)
	}
}
