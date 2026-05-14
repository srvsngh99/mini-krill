package agent

import "testing"

func TestClusterFor_StableID(t *testing.T) {
	a := ClusterFor("summarize this https://www.youtube.com/watch?v=abc")
	b := ClusterFor("summarize this https://youtu.be/xyz")
	if a.ID != b.ID {
		t.Fatalf("youtube summaries should cluster together: %v vs %v", a, b)
	}
	if a.Verb != "summarize" || a.ObjectClass != "youtube" {
		t.Fatalf("unexpected cluster: %+v", a)
	}
}

func TestClusterFor_DistinctClasses(t *testing.T) {
	a := ClusterFor("summarize this https://www.youtube.com/watch?v=abc")
	b := ClusterFor("summarize this https://example.com/article")
	if a.ID == b.ID {
		t.Fatalf("youtube and generic web should not collapse to same cluster")
	}
}

func TestClusterFor_Greeting(t *testing.T) {
	c := ClusterFor("hi")
	if c.Verb != "chat" || c.ObjectClass != "freeform" {
		t.Fatalf("greeting should fall back to chat/freeform, got %+v", c)
	}
}

func TestClusterFor_ErrorText(t *testing.T) {
	c := ClusterFor("debug this error: panic at runtime")
	if c.Verb != "debug" || c.ObjectClass != "error-text" {
		t.Fatalf("expected debug/error-text, got %+v", c)
	}
}

func TestClusterFor_Question(t *testing.T) {
	c := ClusterFor("what should we build?")
	if c.ObjectClass != "question" {
		t.Fatalf("expected question class, got %+v", c)
	}
}

func TestClusterFor_EmptyInput(t *testing.T) {
	c := ClusterFor("   ")
	if c.Verb != "chat" || c.ObjectClass != "freeform" {
		t.Fatalf("expected chat/freeform fallback, got %+v", c)
	}
}

func TestClusterFor_StringRender(t *testing.T) {
	c := ClusterFor("summarize this https://youtube.com/watch?v=abc")
	if c.String() != "summarize/youtube" {
		t.Fatalf("unexpected String(): %q", c.String())
	}
}
