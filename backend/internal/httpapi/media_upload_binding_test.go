package httpapi

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
)

// An image upload can associate itself with a project resource by posting IDs
// alongside the file. Rejections must not overwrite a target, create an asset,
// retain a temporary cache reference, or leave uploaded bytes on disk.

func TestUploadRejectsMalformedBindingIDs(t *testing.T) {
	server, router := testServerRouter(t)
	for _, key := range []string{"character_id", "scene_id", "prop_id", "storyboard_id", "episode_id", "drama_id"} {
		for _, raw := range []string{"0", "-1", "abc", "1.5", "1e3"} {
			recorder := performImageUpload(t, router, map[string]string{key: raw})
			assertUploadError(t, recorder, "invalid "+key)
		}
	}
	assertRejectedUploadsLeaveNoSideEffects(t, server, 0)
}

func TestUploadRejectsMissingBindingTargets(t *testing.T) {
	server, router := testServerRouter(t)
	cases := map[string]string{
		"character_id":  "character not found",
		"scene_id":      "scene not found",
		"prop_id":       "prop not found",
		"storyboard_id": "storyboard not found",
		"episode_id":    "episode not found",
		"drama_id":      "drama not found",
	}
	for key, want := range cases {
		recorder := performImageUpload(t, router, map[string]string{key: "999999"})
		assertUploadError(t, recorder, want)
	}
	assertRejectedUploadsLeaveNoSideEffects(t, server, 0)
}

func TestUploadRejectsBindingAcrossOrganizationsWithoutSideEffects(t *testing.T) {
	now := response.Now()
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	_, _, organizationA := createTestActorSession(t, server, "upload-a@example.com", "upload-a", "owner")
	cookieB, csrfB, organizationB := createTestActorSession(t, server, "upload-b@example.com", "upload-b", "owner")
	router := server.Router()

	drama := models.Drama{OrganizationID: organizationA.ID, Title: "private drama", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: organizationA.ID, DramaID: drama.ID, EpisodeNumber: 1, Title: "private episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{OrganizationID: organizationA.ID, DramaID: drama.ID, Name: "Lin", CreatedAt: now, UpdatedAt: now}
	scene := models.Scene{OrganizationID: organizationA.ID, DramaID: drama.ID, Location: "station", Time: "dusk", Prompt: "p", Status: "pending", CreatedAt: now, UpdatedAt: now}
	prop := models.Prop{OrganizationID: organizationA.ID, DramaID: drama.ID, Name: "ticket", CreatedAt: now, UpdatedAt: now}
	storyboard := models.Storyboard{OrganizationID: organizationA.ID, EpisodeID: episode.ID, StoryboardNumber: 1, Status: "pending", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&character, &scene, &prop, &storyboard} {
		if err := db.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		key  string
		id   uint
		want string
	}{
		{key: "character_id", id: character.ID, want: "character not found"},
		{key: "scene_id", id: scene.ID, want: "scene not found"},
		{key: "prop_id", id: prop.ID, want: "prop not found"},
		{key: "storyboard_id", id: storyboard.ID, want: "storyboard not found"},
		{key: "episode_id", id: episode.ID, want: "episode not found"},
		{key: "drama_id", id: drama.ID, want: "drama not found"},
	}
	for _, tc := range cases {
		recorder := performAuthenticatedImageUpload(t, router, map[string]string{tc.key: strconv.FormatUint(uint64(tc.id), 10)}, cookieB, csrfB)
		// "not found" is intentional: callers must not distinguish a foreign
		// resource from a missing one.
		assertUploadError(t, recorder, tc.want)
	}

	var storedCharacter models.Character
	if err := db.DB.First(&storedCharacter, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedCharacter.ImageURL != "" || storedCharacter.LocalPath != "" {
		t.Fatalf("foreign character was modified: url=%q path=%q", storedCharacter.ImageURL, storedCharacter.LocalPath)
	}
	var storedScene models.Scene
	if err := db.DB.First(&storedScene, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedScene.ImageURL != "" || storedScene.LocalPath != "" || storedScene.Status != "pending" {
		t.Fatalf("foreign scene was modified: url=%q path=%q status=%q", storedScene.ImageURL, storedScene.LocalPath, storedScene.Status)
	}
	var storedProp models.Prop
	if err := db.DB.First(&storedProp, prop.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedProp.ImageURL != "" || storedProp.LocalPath != "" {
		t.Fatalf("foreign prop was modified: url=%q path=%q", storedProp.ImageURL, storedProp.LocalPath)
	}
	assertRejectedUploadsLeaveNoSideEffects(t, server, organizationB.ID)
}

func TestUploadRejectsCharacterBoundToForeignEpisode(t *testing.T) {
	now := response.Now()
	server, router := testServerRouter(t)

	dramaA := models.Drama{Title: "drama A", CreatedAt: now, UpdatedAt: now}
	dramaB := models.Drama{Title: "drama B", CreatedAt: now, UpdatedAt: now}
	for _, drama := range []*models.Drama{&dramaA, &dramaB} {
		if err := db.DB.Create(drama).Error; err != nil {
			t.Fatal(err)
		}
	}
	character := models.Character{DramaID: dramaA.ID, Name: "Lin", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: dramaB.ID, EpisodeNumber: 1, Title: "ep1", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performImageUpload(t, router, map[string]string{
		"character_id": strconv.FormatUint(uint64(character.ID), 10),
		"episode_id":   strconv.FormatUint(uint64(episode.ID), 10),
	})
	assertUploadError(t, recorder, "character does not belong to episode")
	var stored models.Character
	if err := db.DB.First(&stored, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ImageURL != "" || stored.LocalPath != "" {
		t.Fatalf("character was modified: url=%q path=%q", stored.ImageURL, stored.LocalPath)
	}
	assertRejectedUploadsLeaveNoSideEffects(t, server, 0)
}

func TestUploadBindsCharacterAndDerivesDrama(t *testing.T) {
	now := response.Now()
	_, router := testServerRouter(t)

	drama := models.Drama{Title: "bind ok", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	character := models.Character{DramaID: drama.ID, Name: "Lin", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performImageUpload(t, router, map[string]string{"character_id": strconv.FormatUint(uint64(character.ID), 10)})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var updated models.Character
	if err := db.DB.First(&updated, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.ImageURL == "" || updated.LocalPath == "" {
		t.Fatalf("character image was not bound: url=%q path=%q", updated.ImageURL, updated.LocalPath)
	}
	var asset models.Asset
	if err := db.DB.Where("local_path = ?", updated.LocalPath).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if asset.Category != "character" {
		t.Errorf("asset category = %q, want character", asset.Category)
	}
	if asset.DramaID == nil || *asset.DramaID != drama.ID {
		t.Errorf("asset drama = %v, want %d", asset.DramaID, drama.ID)
	}
}

func TestUploadBindsSceneAndMarksCompleted(t *testing.T) {
	now := response.Now()
	_, router := testServerRouter(t)

	drama := models.Drama{Title: "scene bind", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{DramaID: drama.ID, Location: "station", Time: "dusk", Prompt: "p", Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	recorder := performImageUpload(t, router, map[string]string{"scene_id": strconv.FormatUint(uint64(scene.ID), 10)})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var updated models.Scene
	if err := db.DB.First(&updated, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != "completed" || updated.ImageURL == "" {
		t.Fatalf("scene was not completed: status=%q image=%q", updated.Status, updated.ImageURL)
	}
}

func TestUploadUsesSuppliedNameAndCategory(t *testing.T) {
	router := testRouter(t)
	recorder := performImageUpload(t, router, map[string]string{"name": "封面图", "category": "cover"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	data := decodeResponse(t, recorder)["data"].(map[string]any)
	var asset models.Asset
	if err := db.DB.Where("local_path = ?", data["path"]).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if asset.Name != "封面图" || asset.Category != "cover" {
		t.Fatalf("asset name=%q category=%q", asset.Name, asset.Category)
	}
}

func TestUploadWithoutFileIsRejected(t *testing.T) {
	router := testRouter(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("category", "upload"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/image", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assertUploadError(t, recorder, "file is required")
}

func assertUploadError(t *testing.T, recorder *httptest.ResponseRecorder, wantMessage string) {
	t.Helper()
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (body=%s)", recorder.Code, recorder.Body.String())
	}
	payload := decodeResponse(t, recorder)
	if payload["code"] != float64(http.StatusBadRequest) {
		t.Fatalf("response code=%v, want 400", payload["code"])
	}
	if message, _ := payload["message"].(string); message != wantMessage {
		t.Fatalf("message=%q, want %q", message, wantMessage)
	}
}

func assertRejectedUploadsLeaveNoSideEffects(t *testing.T, server *Server, expectedOrganizationID uint) {
	t.Helper()
	var assetCount, referenceCount int64
	if err := db.DB.Model(&models.Asset{}).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&models.MediaCacheReference{}).Where("namespace = ?", "image_upload").Count(&referenceCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 0 || referenceCount != 0 {
		t.Fatalf("rejected upload side effects: assets=%d temporary_references=%d", assetCount, referenceCount)
	}
	var objects []models.MediaCacheObject
	if err := db.DB.Find(&objects).Error; err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if object.OrganizationID != expectedOrganizationID || object.ReferenceCount != 0 || object.Status != mediacache.StatusOrphaned {
			t.Fatalf("unexpected rejected-upload cache object: %#v", object)
		}
		if object.LocalPath != "" {
			if _, err := os.Stat(server.Store.Abs(object.LocalPath)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected upload file still exists at %q (err=%v)", object.LocalPath, err)
			}
		}
	}
	if err := filepath.Walk(server.Store.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("rejected upload left file: " + path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func performAuthenticatedImageUpload(t *testing.T, handler http.Handler, fields map[string]string, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var imageBody bytes.Buffer
	if err := png.Encode(&imageBody, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "valid.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBody.Bytes()); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/image", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Cookie", cookie)
	request.Header.Set("X-CSRF-Token", csrf)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
