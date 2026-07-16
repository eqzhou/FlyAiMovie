package generation

import "testing"

func TestValidateOutputRel(t *testing.T) {
	valid := []string{"composed/shot_1.mp4", "merged/episode_2.mp4"}
	for _, rel := range valid {
		if err := validateOutputRel(rel); err != nil {
			t.Fatalf("valid %q rejected: %v", rel, err)
		}
	}
	invalid := []string{"../escape.mp4", "composed/../../escape.mp4", "/tmp/out.mp4", "other/out.mp4", "merged\\..\\escape.mp4"}
	for _, rel := range invalid {
		if err := validateOutputRel(rel); err == nil {
			t.Fatalf("unsafe %q accepted", rel)
		}
	}
}
