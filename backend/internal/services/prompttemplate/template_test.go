package prompttemplate

import "testing"

func TestRenderInterpolatesApprovedVariables(t *testing.T) {
	got, err := Render("为 {{drama_title}} 的 {{episode_title}} 执行：{{user_instruction}}", map[string]string{
		"drama_title": "归途", "episode_title": "重逢", "user_instruction": "重写对白",
	})
	if err != nil || got != "为 归途 的 重逢 执行：重写对白" {
		t.Fatalf("Render() = %q, %v", got, err)
	}
}

func TestRenderRejectsMalformedUnknownAndMissingVariables(t *testing.T) {
	tests := []struct {
		name    string
		content string
		values  map[string]string
	}{
		{name: "unknown", content: "{{secret}}", values: map[string]string{"secret": "x"}},
		{name: "missing", content: "{{drama_title}}", values: map[string]string{}},
		{name: "unclosed", content: "{{drama_title", values: map[string]string{"drama_title": "x"}},
		{name: "expression", content: "{{drama_title.trim()}}", values: map[string]string{"drama_title": "x"}},
		{name: "unknown value", content: "plain", values: map[string]string{"secret": "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Render(tc.content, tc.values); err == nil {
				t.Fatal("expected template to be rejected")
			}
		})
	}
}

func TestVariablesReturnsStableUniqueNames(t *testing.T) {
	got, err := Variables("{{drama_title}} / {{episode_title}} / {{drama_title}}")
	if err != nil || len(got) != 2 || got[0] != "drama_title" || got[1] != "episode_title" {
		t.Fatalf("Variables() = %#v, %v", got, err)
	}
}

func TestBuiltInDefaultsAreComplete(t *testing.T) {
	items := Defaults()
	if len(items) != 11 {
		t.Fatalf("Defaults() returned %d items", len(items))
	}
	categories := map[string]bool{}
	for _, item := range items {
		got, ok := DefaultFor(item.Key)
		if !ok || got.Content == "" || got.Category == "" {
			t.Fatalf("invalid default: %#v", got)
		}
		categories[got.Category] = true
	}
	for _, category := range []string{"agent_system", "image", "video", "grid"} {
		if !categories[category] {
			t.Fatalf("missing default category %q", category)
		}
	}
	if _, ok := DefaultFor("unknown"); ok {
		t.Fatal("unknown default reported as present")
	}
}

func TestGenerationTemplateVariables(t *testing.T) {
	content := "{{shot_title}} {{shot_description}} {{image_prompt}} {{video_prompt}} {{grid_rows}}x{{grid_cols}} {{grid_mode}}"
	variables, err := Variables(content)
	if err != nil || len(variables) != 7 {
		t.Fatalf("Variables() = %#v, %v", variables, err)
	}
}

func TestAssetImageTemplateVariables(t *testing.T) {
	content := "{{character_name}} {{character_role}} {{character_appearance}} {{character_description}} {{character_personality}} {{scene_location}} {{scene_time}} {{scene_prompt}} {{prop_name}} {{prop_type}} {{prop_description}} {{prop_prompt}}"
	variables, err := Variables(content)
	if err != nil || len(variables) != 12 {
		t.Fatalf("Variables() = %#v, %v", variables, err)
	}
}

func TestApprovedVariablesCoversWhitelist(t *testing.T) {
	got := ApprovedVariables()
	if len(got) != len(approvedVariables) {
		t.Fatalf("ApprovedVariables() len=%d want %d", len(got), len(approvedVariables))
	}
	for _, name := range got {
		if _, ok := approvedVariables[name]; !ok {
			t.Fatalf("ApprovedVariables returned unknown %q", name)
		}
	}
}
