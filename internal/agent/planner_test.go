package agent

import "testing"

func TestDetectStepBlocker(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   string
	}{
		// Structured signals.
		{"explicit need_input tag", "NEED_INPUT: send me the URL", "need_input"},
		{"explicit blocked tag", "BLOCKED: web search not permitted", "blocked"},

		// need_input prose prefixes.
		{"no url provided", "No URL was provided.\nSend it and I'll fetch it.", "need_input"},
		{"still need", "I still need the source material to continue.", "need_input"},

		// Prose blockers added for the 2026-05-16 AI-digest dive, which
		// reported "4/5 steps succeeded" while producing nothing.
		{"result blocked verdict", "Working on it.\n**Result: research step blocked.**\nNeed search access.", "blocked"},
		{"cant actually", "I can't actually pull live results right now.", "blocked"},
		{"cannot actually", "I cannot actually run that search this turn.", "blocked"},
		{"cant pull live", "I can't pull live news without web access.", "blocked"},
		{"same blockers", "Same blockers as step 2 — nothing changed.", "blocked"},
		{"markdown wrapped result", "*Result: gap-fill search blocked.*", "blocked"},

		// Genuine successes must NOT be misclassified.
		{"real answer", "The current time is 14:05 UTC.", ""},
		{"mentions blocked deep in text", "Line1\nLine2\nLine3\nLine4\nLine5\nLine6\nLine7: the road was blocked", ""},
		{"summary that worked", "Here is your digest:\n- Item one\n- Item two", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectStepBlocker(tc.output); got != tc.want {
				t.Errorf("detectStepBlocker(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}
