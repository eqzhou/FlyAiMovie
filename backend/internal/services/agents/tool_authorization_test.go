package agents

import "testing"

// allowedAgentTool is the authorization boundary for tool calls: a model that
// asks for a tool outside its own agent type must be refused, so a compromised
// or confused prompt cannot reach another agent's write tools.

func TestAllowedAgentToolAcceptsOwnTools(t *testing.T) {
	permitted := map[string][]string{
		"script_rewriter":       {"read_episode_script", "save_script"},
		"extractor":             {"read_script_for_extraction", "read_existing_characters", "read_existing_scenes", "save_dedup_characters", "save_dedup_scenes"},
		"storyboard_breaker":    {"read_storyboard_context", "save_storyboards"},
		"voice_assigner":        {"list_voices", "get_characters", "assign_voice"},
		"grid_prompt_generator": {"read_characters", "read_scenes", "read_shots_for_grid", "generate_grid_prompt"},
	}

	for agentType, tools := range permitted {
		for _, tool := range tools {
			if !allowedAgentTool(agentType, tool) {
				t.Errorf("%s was denied its own tool %q", agentType, tool)
			}
		}
	}
}

func TestAllowedAgentToolRejectsCrossAgentTools(t *testing.T) {
	// Every agent's tools must be rejected for every other agent. Writes are
	// the dangerous case, but reads are scoped too, so no pair may leak.
	permitted := map[string][]string{
		"script_rewriter":       {"read_episode_script", "save_script"},
		"extractor":             {"read_script_for_extraction", "save_dedup_characters", "save_dedup_scenes"},
		"storyboard_breaker":    {"read_storyboard_context", "save_storyboards"},
		"voice_assigner":        {"list_voices", "assign_voice"},
		"grid_prompt_generator": {"read_shots_for_grid", "generate_grid_prompt"},
	}

	for owner, tools := range permitted {
		for other := range permitted {
			if other == owner {
				continue
			}
			for _, tool := range tools {
				if allowedAgentTool(other, tool) {
					t.Errorf("%s was allowed %s's tool %q", other, owner, tool)
				}
			}
		}
	}
}

func TestAllowedAgentToolRejectsUnknownInput(t *testing.T) {
	cases := []struct{ agentType, tool string }{
		{agentType: "script_rewriter", tool: "drop_database"},
		{agentType: "script_rewriter", tool: ""},
		{agentType: "unknown_agent", tool: "save_script"},
		{agentType: "", tool: "save_script"},
		{agentType: "", tool: ""},
		// Names are matched exactly: no prefix, suffix or case slack.
		{agentType: "script_rewriter", tool: "save_script "},
		{agentType: "script_rewriter", tool: "SAVE_SCRIPT"},
		{agentType: "script_rewriter", tool: "save_script_extra"},
		{agentType: "Script_Rewriter", tool: "save_script"},
	}

	for _, tc := range cases {
		if allowedAgentTool(tc.agentType, tc.tool) {
			t.Errorf("allowedAgentTool(%q, %q) = true, want false", tc.agentType, tc.tool)
		}
	}
}
