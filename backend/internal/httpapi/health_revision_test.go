package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// scripts/deploy-status.sh reads the revision out of this response to detect a
// stale PM2 deploy. That makes the field name part of the contract, not just a
// diagnostic, so it is pinned here.

func TestHealthReportsBuildRevision(t *testing.T) {
	original := BuildRevision
	t.Cleanup(func() { BuildRevision = original })
	BuildRevision = "abc1234"

	response := performRequest(testRouter(t), http.MethodGet, "/api/v1/health", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	var body struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
		Revision  string `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, response.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Timestamp == "" {
		t.Error("timestamp is empty")
	}
	if body.Revision != "abc1234" {
		t.Errorf("revision = %q, want abc1234", body.Revision)
	}
}

func TestHealthRevisionDefaultsToUnknown(t *testing.T) {
	// A binary built without the -ldflags override must still answer, and must
	// say so plainly: deploy-status.sh treats "unknown" as stale.
	original := BuildRevision
	t.Cleanup(func() { BuildRevision = original })
	BuildRevision = "unknown"

	response := performRequest(testRouter(t), http.MethodGet, "/api/v1/health", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	var body struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != "unknown" {
		t.Errorf("revision = %q, want unknown", body.Revision)
	}
}
