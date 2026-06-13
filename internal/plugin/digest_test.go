package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAIDigestSkillFetchesRealSources(t *testing.T) {
	hn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "tags=story") {
			t.Errorf("expected story filter in query, got %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"hits":[
			{"title":"New OSS model released","url":"https://example.com/model","points":120,"objectID":"1"},
			{"title":"Ask HN: AI tooling","url":"","points":40,"objectID":"42"}
		]}`))
	}))
	defer hn.Close()

	s := &aiDigestSkill{
		hnBase: hn.URL,
		searchFn: func(_ context.Context, _ string) ([]searchResult, error) {
			return []searchResult{{title: "AI weekly roundup", url: "https://example.com/roundup", snippet: "the week in AI"}}, nil
		},
	}

	out, err := s.Execute(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for _, want := range []string{
		"New OSS model released",
		"https://example.com/model",
		"https://news.ycombinator.com/item?id=42", // URL-less story falls back to HN link
		"AI weekly roundup",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("digest output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestAIDigestSkillSurvivesOneDeadSource(t *testing.T) {
	s := &aiDigestSkill{
		hnBase: "http://127.0.0.1:1", // unreachable
		searchFn: func(_ context.Context, _ string) ([]searchResult, error) {
			return []searchResult{{title: "Only web result", url: "https://example.com/x", snippet: "s"}}, nil
		},
	}
	out, err := s.Execute(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("Execute should tolerate one dead source, got error: %v", err)
	}
	if !strings.Contains(out, "Only web result") {
		t.Errorf("expected web results in output, got:\n%s", out)
	}
}

func TestAIDigestSkillErrorsWhenAllSourcesDead(t *testing.T) {
	s := &aiDigestSkill{
		hnBase: "http://127.0.0.1:1",
		searchFn: func(_ context.Context, _ string) ([]searchResult, error) {
			return nil, context.DeadlineExceeded
		},
	}
	if _, err := s.Execute(context.Background(), "", nil); err == nil {
		t.Fatal("expected error when every source is unreachable")
	}
}
