package agent

import "testing"

func TestDetectRecallRequest_LastN(t *testing.T) {
	cases := map[string]int{
		"check our last 10 messages":   10,
		"go back 5 messages":           5,
		"show me the last 20 messages": 20,
	}
	for input, wantN := range cases {
		req := detectRecallRequest(input)
		if req.kind != "lastN" || req.n != wantN {
			t.Errorf("detectRecallRequest(%q) = {kind:%q, n:%d}, want {lastN, %d}", input, req.kind, req.n, wantN)
		}
	}
}

func TestDetectRecallRequest_Yesterday(t *testing.T) {
	if req := detectRecallRequest("what did I say yesterday"); req.kind != "yesterday" {
		t.Errorf("expected yesterday, got %+v", req)
	}
	if req := detectRecallRequest("what did we say yesterday"); req.kind != "yesterday" {
		t.Errorf("expected yesterday for 'we', got %+v", req)
	}
}

func TestDetectRecallRequest_Today(t *testing.T) {
	if req := detectRecallRequest("what did I say today"); req.kind != "today" {
		t.Errorf("expected today, got %+v", req)
	}
}

func TestDetectRecallRequest_Topic(t *testing.T) {
	cases := []struct {
		input string
		topic string
	}{
		{"what did I say about the AI digest", "the AI digest"},
		{"remember when we discussed the search engine", "the search engine"},
		{"earlier you said something about the planner", "something about the planner"},
	}
	for _, tc := range cases {
		req := detectRecallRequest(tc.input)
		if req.kind != "topic" {
			t.Errorf("detectRecallRequest(%q).kind = %q, want topic", tc.input, req.kind)
		}
		if req.topic != tc.topic {
			t.Errorf("detectRecallRequest(%q).topic = %q, want %q", tc.input, req.topic, tc.topic)
		}
	}
}

func TestDetectRecallRequest_NoMatch(t *testing.T) {
	cases := []string{"hi", "what's the weather", "how are you", "tell me a joke"}
	for _, input := range cases {
		if req := detectRecallRequest(input); req.kind != "" {
			t.Errorf("detectRecallRequest(%q) should not match, got %+v", input, req)
		}
	}
}
