package httpapi

import (
	"net/http"
	"testing"
)

func TestBusinessEndpointsReturnStableClientErrors(t *testing.T) {
	router := testRouter(t)
	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/api/v1/dramas", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/dramas/999", "", http.StatusNotFound},
		{http.MethodPut, "/api/v1/dramas/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/dramas/999", `{"title":"x"}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/dramas/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/episodes", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/episodes/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/episodes/999", `{"title":"x"}`, http.StatusNotFound},
		{http.MethodGet, "/api/v1/episodes/999/characters", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/episodes/999/scenes", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/episodes/999/storyboards", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/episodes/999/pipeline-status", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/characters", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/characters/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/characters/999", `{"name":"x"}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/characters/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/characters/999/generate-image", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/characters/batch-generate-images", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/scenes", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/scenes/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/scenes/999", `{"location":"x"}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/scenes/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/scenes/999/generate-image", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/scenes/999/copy", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/storyboards", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/bad", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/storyboards/999", `{"title":"x"}`, http.StatusNotFound},
		{http.MethodDelete, "/api/v1/storyboards/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/storyboards/999/generate-tts", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/storyboards/999/generate-frame", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/storyboards/999/generate-video", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-frames", `{`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-videos", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/storyboards/batch-generate-tts", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/images", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/images", `{"prompt":"x"}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/images/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/videos", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/videos", `{"prompt":"x"}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/videos/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/compose/storyboards/999/compose", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/compose/episodes/999/compose-all", `{}`, http.StatusNotFound},
		{http.MethodGet, "/api/v1/compose/episodes/999/compose-status", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/merge/episodes/999/merge", `{}`, http.StatusNotFound},
		{http.MethodGet, "/api/v1/merge/episodes/999/merge", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/grid/prompt", `{"rows":9,"cols":9}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/grid/generate", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/grid/status/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/grid/split", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/grid/history/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/character-library", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/character-library/bad", `{}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/character-library/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/character-library/999/import", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/props", `{}`, http.StatusBadRequest},
		{http.MethodPut, "/api/v1/props/bad", `{}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/props/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/props/999/generate-image", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/assets", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/assets/bad", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/assets/999", "", http.StatusNotFound},
		{http.MethodPut, "/api/v1/assets/bad", `{}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/assets/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/assets/999/apply", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/assets/999/probe", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/upload/media", "", http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/ai-configs/999", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/agent-configs", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/agent-configs/999", "", http.StatusBadRequest},
		{http.MethodPut, "/api/v1/agent-configs/bad", `{}`, http.StatusBadRequest},
		{http.MethodDelete, "/api/v1/agent-configs/999", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/agent/unknown/debug", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent/unknown/chat", `{}`, http.StatusBadRequest},
		{http.MethodGet, "/api/v1/agent-runs?episode_id=bad", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/agent-runs/bad", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/agent-runs/999/cancel", `{}`, http.StatusNotFound},
		{http.MethodGet, "/api/v1/jobs/bad", "", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/jobs/999", "", http.StatusNotFound},
		{http.MethodGet, "/api/v1/jobs/999/events", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/jobs/batch-cancel", `{}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v1/jobs/999/cancel", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/jobs/999/retry", `{}`, http.StatusNotFound},
		{http.MethodGet, "/api/v1/organization/members", "", http.StatusForbidden},
		{http.MethodPut, "/api/v1/organization/members/1", `{"role":"viewer"}`, http.StatusForbidden},
		{http.MethodGet, "/api/v1/organization/members/invitations", "", http.StatusForbidden},
		{http.MethodDelete, "/api/v1/organization/members/invitations/1", "", http.StatusForbidden},
		{http.MethodGet, "/api/v1/organization/export", "", http.StatusForbidden},
		{http.MethodDelete, "/api/v1/organization", "", http.StatusForbidden},
	}
	for _, test := range tests {
		result := performRequest(router, test.method, test.path, test.body, nil)
		if result.Code != test.status {
			t.Fatalf("%s %s status=%d want=%d body=%s", test.method, test.path, result.Code, test.status, result.Body.String())
		}
		if result.Code >= http.StatusInternalServerError {
			t.Fatalf("%s %s returned server error: %s", test.method, test.path, result.Body.String())
		}
	}
}
