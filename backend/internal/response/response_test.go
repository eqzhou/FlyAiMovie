package response

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponseHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		fn   func(*gin.Context)
		code int
	}{
		{"success", func(c *gin.Context) { Success(c, map[string]any{"ok": true}) }, 200},
		{"created", func(c *gin.Context) { Created(c, map[string]any{"id": 1}) }, 201},
		{"bad", func(c *gin.Context) { BadRequest(c, "") }, 400},
		{"notfound", func(c *gin.Context) { NotFound(c, "") }, 404},
		{"conflict", func(c *gin.Context) { Conflict(c, "") }, 409},
		{"server", func(c *gin.Context) { ServerError(c, "") }, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			tc.fn(c)
			if r.Code != tc.code {
				t.Fatalf("status=%d want %d", r.Code, tc.code)
			}
			if r.Body.Len() == 0 {
				t.Fatal("empty response")
			}
		})
	}
	if Now() == "" {
		t.Fatal("Now returned empty string")
	}
}

func TestSuccessSerializesNilSliceAsEmptyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	var rows []string
	Success(context, rows)

	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if string(payload.Data) != "[]" {
		t.Fatalf("data=%s want []", payload.Data)
	}
}
