package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestGridRequestsRejectUnsafeStoryboardCountsBeforeGeneration(t *testing.T) {
	router := testRouter(t)

	underfilledFirstLast := performRequest(router, http.MethodPost, "/api/v1/grid/generate", `{
		"prompt":"first and last frames","mode":" first_last ","rows":2,"cols":2,
		"storyboard_ids":[1,1]
	}`, nil)
	if underfilledFirstLast.Code != http.StatusBadRequest || !strings.Contains(underfilledFirstLast.Body.String(), "matching grid cells") {
		t.Fatalf("underfilled first_last status=%d body=%s", underfilledFirstLast.Code, underfilledFirstLast.Body.String())
	}

	tooManyIDs := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{
		"rows":1,"cols":1,"frame_type":"first_frame","image_url":"/static/missing.png",
		"storyboard_ids":[1,2]
	}`, nil)
	if tooManyIDs.Code != http.StatusBadRequest || !strings.Contains(tooManyIDs.Body.String(), "at most 1 storyboard ids") {
		t.Fatalf("too many storyboard ids status=%d body=%s", tooManyIDs.Code, tooManyIDs.Body.String())
	}

	duplicateIDs := performRequest(router, http.MethodPost, "/api/v1/grid/generate", `{
		"prompt":"duplicate targets","mode":"first_frame","rows":1,"cols":2,
		"storyboard_ids":[1,1]
	}`, nil)
	if duplicateIDs.Code != http.StatusBadRequest || !strings.Contains(duplicateIDs.Body.String(), "unique") {
		t.Fatalf("duplicate storyboard ids status=%d body=%s", duplicateIDs.Code, duplicateIDs.Body.String())
	}

	tooManyCellPrompts := performRequest(router, http.MethodPost, "/api/v1/grid/generate", `{
		"prompt":"too many cell prompts","mode":"first_frame","rows":1,"cols":1,
		"cell_prompts":["one","two"]
	}`, nil)
	if tooManyCellPrompts.Code != http.StatusBadRequest || !strings.Contains(tooManyCellPrompts.Body.String(), "cell_prompts") {
		t.Fatalf("too many cell prompts status=%d body=%s", tooManyCellPrompts.Code, tooManyCellPrompts.Body.String())
	}

	oversizedBody := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{"rows":1,"cols":1,"image_url":"/static/missing.png","padding":"`+strings.Repeat("x", 140<<10)+`"}`, nil)
	if oversizedBody.Code != http.StatusBadRequest {
		t.Fatalf("oversized split status=%d body=%s", oversizedBody.Code, oversizedBody.Body.String())
	}
	oversizedPromptBody := performRequest(router, http.MethodPost, "/api/v1/grid/prompt", `{"rows":1,"cols":1,"padding":"`+strings.Repeat("x", 140<<10)+`"}`, nil)
	if oversizedPromptBody.Code != http.StatusBadRequest {
		t.Fatalf("oversized prompt status=%d body=%s", oversizedPromptBody.Code, oversizedPromptBody.Body.String())
	}
}

func TestGridCellCanBeReassignedAndAssignmentPersists(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "grid assignment", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	otherEpisode := models.Episode{DramaID: drama.ID, EpisodeNumber: 2, Title: "two", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&otherEpisode).Error; err != nil {
		t.Fatal(err)
	}
	first := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "first", FirstFrameImage: "/static/cell-1.png", CreatedAt: now, UpdatedAt: now}
	second := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 2, Title: "second", FirstFrameImage: "/static/cell-2.png", CreatedAt: now, UpdatedAt: now}
	foreign := models.Storyboard{EpisodeID: otherEpisode.ID, StoryboardNumber: 1, Title: "foreign", CreatedAt: now, UpdatedAt: now}
	for _, storyboard := range []*models.Storyboard{&first, &second, &foreign} {
		if err := db.DB.Create(storyboard).Error; err != nil {
			t.Fatal(err)
		}
	}
	history := models.GridHistory{
		DramaID: &drama.ID, EpisodeID: &episode.ID, Mode: "first_frame", Rows: 1, Cols: 2,
		ImageURL: "/static/grid.png", CellsJSON: `["/static/cell-1.png","/static/cell-2.png"]`,
		StoryboardIDs: `[` + itoa(first.ID) + `,` + itoa(second.ID) + `]`, Status: "split", CreatedAt: now, UpdatedAt: now,
		AssignmentsJSON: `[{"cell_index":0,"storyboard_id":` + itoa(first.ID) + `,"frame_type":"first_frame"},{"cell_index":1,"storyboard_id":` + itoa(second.ID) + `,"frame_type":"first_frame"}]`,
		CellsVerified:   true,
	}
	if err := db.DB.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	reSplit := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{"history_id":`+itoa(history.ID)+`,"image_url":"/static/grid.png","rows":1,"cols":2,"frame_type":"first_frame"}`, nil)
	if reSplit.Code != http.StatusConflict || !strings.Contains(reSplit.Body.String(), "already split") {
		t.Fatalf("repeat split status=%d body=%s", reSplit.Code, reSplit.Body.String())
	}
	for index, url := range []string{"/static/cell-1.png", "/static/cell-2.png"} {
		asset := models.Asset{OrganizationID: history.OrganizationID, DramaID: &drama.ID, EpisodeID: &episode.ID, GridHistoryID: &history.ID, Name: "cell " + itoa(uint(index+1)), Type: "image", Category: "grid_cell", URL: url, CreatedAt: now, UpdatedAt: now}
		if err := db.DB.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
	}

	assigned := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{
		"cell_index":1,"storyboard_id":`+itoa(first.ID)+`,"frame_type":"first_frame"
	}`, nil)
	if assigned.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", assigned.Code, assigned.Body.String())
	}
	if err := db.DB.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.FirstFrameImage != "/static/cell-2.png" {
		t.Fatalf("first frame=%q", first.FirstFrameImage)
	}
	if err := db.DB.First(&second, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if second.FirstFrameImage != "" {
		t.Fatalf("old target was not cleared: %q", second.FirstFrameImage)
	}
	assigned = performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{
		"cell_index":1,"storyboard_id":`+itoa(first.ID)+`,"frame_type":"last_frame"
	}`, nil)
	if assigned.Code != http.StatusOK {
		t.Fatalf("move to last frame status=%d body=%s", assigned.Code, assigned.Body.String())
	}
	if err := db.DB.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.FirstFrameImage != "" || first.LastFrameImage != "/static/cell-2.png" {
		t.Fatalf("moved frames first=%q last=%q", first.FirstFrameImage, first.LastFrameImage)
	}
	historyResponse := performRequest(router, http.MethodGet, "/api/v1/grid/history/"+itoa(history.ID), "", nil)
	historyPayload := decodeResponse(t, historyResponse)["data"].(map[string]any)
	assignmentsJSON, _ := historyPayload["assignments_json"].(string)
	if historyResponse.Code != http.StatusOK || strings.Contains(assignmentsJSON, `"cell_index":0`) || !strings.Contains(assignmentsJSON, `"cell_index":1`) || !strings.Contains(assignmentsJSON, `"frame_type":"last_frame"`) {
		t.Fatalf("assignment was not persisted: status=%d body=%s", historyResponse.Code, historyResponse.Body.String())
	}
	var audit models.AuditLog
	if err := db.DB.Where("resource_type = ?", "grid_cell_assignment").Last(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.Action != "post.grid_cell_assignment" || !strings.Contains(audit.ResourceID, "history:"+itoa(history.ID)) || !strings.Contains(audit.ResourceID, "storyboard:"+itoa(first.ID)) {
		t.Fatalf("unexpected grid assignment audit: %+v", audit)
	}

	invalidFrame := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":0,"storyboard_id":`+itoa(first.ID)+`,"frame_type":"video"}`, nil)
	if invalidFrame.Code != http.StatusBadRequest {
		t.Fatalf("invalid frame status=%d body=%s", invalidFrame.Code, invalidFrame.Body.String())
	}
	invalidIndex := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":9,"storyboard_id":`+itoa(first.ID)+`,"frame_type":"first_frame"}`, nil)
	if invalidIndex.Code != http.StatusBadRequest {
		t.Fatalf("invalid index status=%d body=%s", invalidIndex.Code, invalidIndex.Body.String())
	}
	crossEpisode := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":0,"storyboard_id":`+itoa(foreign.ID)+`,"frame_type":"first_frame"}`, nil)
	if crossEpisode.Code != http.StatusConflict {
		t.Fatalf("cross episode status=%d body=%s", crossEpisode.Code, crossEpisode.Body.String())
	}
	trailingJSON := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":0,"storyboard_id":`+itoa(first.ID)+`,"frame_type":"first_frame"}{}`, nil)
	if trailingJSON.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d body=%s", trailingJSON.Code, trailingJSON.Body.String())
	}
	invalidSplitFrame := performRequest(router, http.MethodPost, "/api/v1/grid/split", `{"rows":1,"cols":1,"frame_type":"video","image_url":"/static/missing.png"}`, nil)
	if invalidSplitFrame.Code != http.StatusBadRequest || !strings.Contains(invalidSplitFrame.Body.String(), "frame_type") {
		t.Fatalf("invalid split frame status=%d body=%s", invalidSplitFrame.Code, invalidSplitFrame.Body.String())
	}
}

func TestGridSplitRejectsMediaOwnedByAnotherOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	_, _, organizationA := createTestActorSession(t, server, "grid-a@example.com", "grid-a", "owner")
	cookieB, csrfB, organizationB := createTestActorSession(t, server, "grid-b@example.com", "grid-b", "owner")
	now := response.Now()
	relativePath, publicURL, err := server.Store.SaveBytes("uploads", "tenant-a.png", []byte("tenant a media"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.Asset{OrganizationID: organizationA.ID, Name: "tenant A", Type: "image", URL: publicURL, LocalPath: relativePath, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{OrganizationID: organizationB.ID, Title: "tenant B", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: organizationB.ID, DramaID: drama.ID, EpisodeNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	history := models.GridHistory{OrganizationID: organizationB.ID, DramaID: &drama.ID, EpisodeID: &episode.ID, Mode: "first_frame", Rows: 1, Cols: 1, Status: "completed", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&history).Error; err != nil {
		t.Fatal(err)
	}

	result := performAuthRequest(router, http.MethodPost, "/api/v1/grid/split", `{"history_id":`+itoa(history.ID)+`,"image_url":"`+publicURL+`","rows":1,"cols":1,"frame_type":"first_frame"}`, cookieB, csrfB)
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "not owned") {
		t.Fatalf("cross-organization split status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestMutableMediaReferencesCannotClaimUnownedLocalPaths(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "ownership", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{Name: "owned row", Type: "image", Category: "reference", URL: "https://example.test/original.png", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}

	for _, request := range []struct {
		path string
		body string
	}{
		{"/api/v1/storyboards/" + itoa(storyboard.ID), `{"first_frame_image":"/static/other-organization.png"}`},
		{"/api/v1/assets/" + itoa(asset.ID), `{"url":"/static/other-organization.png","category":"grid_cell"}`},
		{"/api/v1/dramas/" + itoa(drama.ID), `{"thumbnail":"/static/other-organization.png"}`},
	} {
		result := performRequest(router, http.MethodPut, request.path, request.body, nil)
		if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "not owned") {
			t.Fatalf("%s status=%d body=%s", request.path, result.Code, result.Body.String())
		}
	}
}


func TestGridAssignMapsBusinessErrors(t *testing.T) {
	router := testRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "grid errors", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "one", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "shot", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}
	history := models.GridHistory{
		DramaID: &drama.ID, EpisodeID: &episode.ID, Mode: "first_frame", Rows: 1, Cols: 1,
		ImageURL: "/static/grid.png", CellsJSON: `["/static/cell-1.png"]`, Status: "split",
		CellsVerified: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.Asset{OrganizationID: history.OrganizationID, DramaID: &drama.ID, EpisodeID: &episode.ID, GridHistoryID: &history.ID, Name: "cell", Type: "image", Category: "grid_cell", URL: "/static/cell-1.png", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	missingStoryboard := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":0,"storyboard_id":99999,"frame_type":"first_frame"}`, nil)
	if missingStoryboard.Code != http.StatusConflict && missingStoryboard.Code != http.StatusNotFound {
		// ownership validation may conflict before not-found; either is better than 500
		t.Fatalf("missing storyboard status=%d body=%s", missingStoryboard.Code, missingStoryboard.Body.String())
	}
	if missingStoryboard.Code == http.StatusInternalServerError {
		t.Fatalf("mapped business error should not be 500: %s", missingStoryboard.Body.String())
	}

	history.CellsJSON = `[]`
	if err := db.DB.Model(&history).Update("cells_json", `[]`).Error; err != nil {
		t.Fatal(err)
	}
	missingCell := performRequest(router, http.MethodPost, "/api/v1/grid/history/"+itoa(history.ID)+"/assign", `{"cell_index":0,"storyboard_id":`+itoa(storyboard.ID)+`,"frame_type":"first_frame"}`, nil)
	if missingCell.Code != http.StatusBadRequest {
		t.Fatalf("missing cell status=%d body=%s", missingCell.Code, missingCell.Body.String())
	}
}
