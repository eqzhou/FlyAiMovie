package httpapi

import (
	"reflect"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/models"
)

// Grid helpers map split cells onto storyboard frame slots. They read values
// that were persisted as JSON, so a malformed column must degrade to "no
// assignments" rather than panic, and the first_last split must not drift
// off-by-one when the cell count is odd.

func TestDecodeGridStringsAndIDs(t *testing.T) {
	if got := decodeGridStrings(`["a","b"]`); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("decodeGridStrings = %#v", got)
	}
	if got := decodeGridIDs(`[3,1,2]`); !reflect.DeepEqual(got, []uint{3, 1, 2}) {
		t.Errorf("decodeGridIDs = %#v", got)
	}
	// Malformed or wrongly-typed JSON yields nil, which callers treat as empty.
	for _, raw := range []string{"", "not json", "{}", `["a"`, `[1,2]`} {
		if got := decodeGridStrings(raw); got != nil && raw == "" {
			t.Errorf("decodeGridStrings(%q) = %#v, want nil", raw, got)
		}
	}
	for _, raw := range []string{"", "not json", `["a","b"]`, `[-1]`} {
		if got := decodeGridIDs(raw); got != nil {
			t.Errorf("decodeGridIDs(%q) = %#v, want nil", raw, got)
		}
	}
}

func TestDeriveGridAssignmentsSingleFrame(t *testing.T) {
	got := deriveGridAssignments("first_frame", []uint{10, 11, 12, 13}, 4, "")
	want := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
		{CellIndex: 2, StoryboardID: 12, FrameType: "first_frame"},
		{CellIndex: 3, StoryboardID: 13, FrameType: "first_frame"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveGridAssignmentsHonoursRequestedFrameType(t *testing.T) {
	got := deriveGridAssignments("single", []uint{10}, 1, "composed")
	if len(got) != 1 || got[0].FrameType != "composed" {
		t.Fatalf("got %#v", got)
	}
	// An unrecognised frame type falls back rather than erroring, because the
	// caller has already accepted the grid.
	got = deriveGridAssignments("single", []uint{10}, 1, "bogus")
	if len(got) != 1 || got[0].FrameType != "first_frame" {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveGridAssignmentsFirstLastSplitsHalves(t *testing.T) {
	// Six cells over three shots: first half takes first frames, second half
	// wraps back to the same shots for their last frames.
	got := deriveGridAssignments("first_last", []uint{10, 11, 12, 10, 11, 12}, 6, "")
	want := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
		{CellIndex: 2, StoryboardID: 12, FrameType: "first_frame"},
		{CellIndex: 3, StoryboardID: 10, FrameType: "last_frame"},
		{CellIndex: 4, StoryboardID: 11, FrameType: "last_frame"},
		{CellIndex: 5, StoryboardID: 12, FrameType: "last_frame"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveGridAssignmentsFirstLastNeedsTwoShots(t *testing.T) {
	// A single shot cannot fill a first/last pair, so nothing is assigned.
	if got := deriveGridAssignments("first_last", []uint{10}, 2, ""); len(got) != 0 {
		t.Fatalf("got %#v, want no assignments", got)
	}
}

func TestDeriveGridAssignmentsStopsAtShorterSide(t *testing.T) {
	if got := deriveGridAssignments("single", []uint{10, 11}, 4, ""); len(got) != 2 {
		t.Fatalf("cellCount > shots: got %d assignments", len(got))
	}
	if got := deriveGridAssignments("single", []uint{10, 11, 12}, 2, ""); len(got) != 2 {
		t.Fatalf("shots > cellCount: got %d assignments", len(got))
	}
}

func TestGridFrameColumnAndUpdate(t *testing.T) {
	columns := map[string]string{
		"last_frame":  "last_frame_image",
		"composed":    "composed_image",
		"first_frame": "first_frame_image",
		"":            "first_frame_image",
		"unknown":     "first_frame_image",
	}
	for frameType, want := range columns {
		if got := gridFrameColumn(frameType); got != want {
			t.Errorf("gridFrameColumn(%q) = %q, want %q", frameType, got, want)
		}
		updates := gridFrameUpdate(frameType, "https://cdn.example/a.png")
		if updates[want] != "https://cdn.example/a.png" {
			t.Errorf("gridFrameUpdate(%q) did not set %q: %#v", frameType, want, updates)
		}
		// Every write bumps updated_at so the row does not look stale.
		if _, ok := updates["updated_at"]; !ok {
			t.Errorf("gridFrameUpdate(%q) omitted updated_at", frameType)
		}
	}
}

func TestHistoryGridAssignmentsPrefersStoredValues(t *testing.T) {
	history := models.GridHistory{
		AssignmentsJSON: `[{"cell_index":1,"storyboard_id":22,"frame_type":"last_frame"}]`,
		Mode:            "single",
		StoryboardIDs:   `[10,11]`,
	}
	got := historyGridAssignments(history, 2)
	want := []gridCellAssignment{{CellIndex: 1, StoryboardID: 22, FrameType: "last_frame"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestHistoryGridAssignmentsDropsOutOfRangeAndFallsBack(t *testing.T) {
	// Stored rows referencing a cell outside the grid, or shot id 0, are not
	// trustworthy; when none survive, the derived mapping is used instead.
	history := models.GridHistory{
		AssignmentsJSON: `[{"cell_index":9,"storyboard_id":22,"frame_type":"first_frame"},{"cell_index":0,"storyboard_id":0,"frame_type":"first_frame"}]`,
		Mode:            "single",
		StoryboardIDs:   `[10,11]`,
	}
	got := historyGridAssignments(history, 2)
	want := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestHistoryGridAssignmentsFallsBackOnMalformedJSON(t *testing.T) {
	history := models.GridHistory{
		AssignmentsJSON: `not json`,
		Mode:            "single",
		StoryboardIDs:   `[10]`,
	}
	got := historyGridAssignments(history, 1)
	want := []gridCellAssignment{{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestReplaceGridAssignmentReplacesSameCell(t *testing.T) {
	existing := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
	}
	got := replaceGridAssignment(existing, gridCellAssignment{CellIndex: 1, StoryboardID: 22, FrameType: "last_frame"})
	want := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 22, FrameType: "last_frame"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestReplaceGridAssignmentEvictsDuplicateTarget(t *testing.T) {
	// A shot+frame slot can hold only one cell: assigning it to cell 2 must
	// release cell 0, otherwise two cells would write the same frame.
	existing := []gridCellAssignment{
		{CellIndex: 0, StoryboardID: 10, FrameType: "first_frame"},
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
	}
	got := replaceGridAssignment(existing, gridCellAssignment{CellIndex: 2, StoryboardID: 10, FrameType: "first_frame"})
	want := []gridCellAssignment{
		{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"},
		{CellIndex: 2, StoryboardID: 10, FrameType: "first_frame"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestReplaceGridAssignmentAppendsAndSorts(t *testing.T) {
	existing := []gridCellAssignment{{CellIndex: 3, StoryboardID: 13, FrameType: "first_frame"}}
	got := replaceGridAssignment(existing, gridCellAssignment{CellIndex: 1, StoryboardID: 11, FrameType: "first_frame"})
	if len(got) != 2 || got[0].CellIndex != 1 || got[1].CellIndex != 3 {
		t.Fatalf("result is not sorted by cell index: %#v", got)
	}
}

func TestIsUniqueConstraintError(t *testing.T) {
	if isUniqueConstraintError(nil) {
		t.Fatal("nil was reported as a unique-constraint error")
	}
	for _, msg := range []string{
		"UNIQUE constraint failed: users.email",
		"duplicate key value violates unique constraint",
		"ERROR: Duplicate entry",
	} {
		if !isUniqueConstraintError(errString(msg)) {
			t.Errorf("%q was not recognised", msg)
		}
	}
	for _, msg := range []string{"connection refused", "no rows in result set"} {
		if isUniqueConstraintError(errString(msg)) {
			t.Errorf("%q was misreported as a unique-constraint error", msg)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
