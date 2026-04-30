package content

import (
	"strings"
	"testing"
)

func TestExtractText(t *testing.T) {
	got := ExtractText(`<html><style>.x{}</style><script>alert(1)</script><body><h1>Hello</h1><!-- hidden --><p>World</p></body></html>`)
	if strings.Contains(got, "alert") || strings.Contains(got, "hidden") {
		t.Fatalf("ExtractText kept script/comment: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Fatalf("ExtractText missing visible text: %q", got)
	}
}

func TestParseSearchHTML(t *testing.T) {
	page := `<a rel="nofollow" class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com">Example</a><a class="result__snippet">Snippet</a>`
	results := parseSearchHTML(page, 5)
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].URL != "https://example.com" {
		t.Fatalf("URL = %q", results[0].URL)
	}
}
