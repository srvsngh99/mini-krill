package content

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractVideoID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"standard watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"short link", "https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"embed", "https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"shorts", "https://www.youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"with params", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "dQw4w9WgXcQ"},
		{"http", "http://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"no www", "https://youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"v path", "https://www.youtube.com/v/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"not youtube", "https://example.com/watch?v=dQw4w9WgXcQ", ""},
		{"empty", "", ""},
		{"no video id", "https://www.youtube.com/", ""},
		{"playlist only", "https://www.youtube.com/playlist?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9efdf", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVideoID(tt.url)
			if got != tt.want {
				t.Errorf("extractVideoID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsYouTubeURL(t *testing.T) {
	if !IsYouTubeURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
		t.Error("expected true for standard YouTube URL")
	}
	if !IsYouTubeURL("https://youtu.be/dQw4w9WgXcQ") {
		t.Error("expected true for short YouTube URL")
	}
	if IsYouTubeURL("https://example.com/page") {
		t.Error("expected false for non-YouTube URL")
	}
}

func TestExtractCaptionURL(t *testing.T) {
	// Simulated player response snippet with caption tracks.
	page := `var ytInitialPlayerResponse = {"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"baseUrl":"https:\/\/www.youtube.com\/api\/timedtext?v=abc\u0026lang=en","name":{"simpleText":"English"},"languageCode":"en"},{"baseUrl":"https:\/\/www.youtube.com\/api\/timedtext?v=abc\u0026lang=es","name":{"simpleText":"Spanish"},"languageCode":"es"}]}}};`

	got := extractCaptionURL(page)
	if got == "" {
		t.Fatal("expected a caption URL, got empty string")
	}
	if !strings.Contains(got, "lang=en") {
		t.Errorf("expected English caption URL, got %q", got)
	}
}

func TestExtractCaptionURL_FallbackToFirst(t *testing.T) {
	// Only non-English captions available.
	page := `"captionTracks":[{"baseUrl":"https://example.com/timedtext?lang=ja","languageCode":"ja"}]`

	got := extractCaptionURL(page)
	if got == "" {
		t.Fatal("expected fallback caption URL, got empty string")
	}
	if !strings.Contains(got, "lang=ja") {
		t.Errorf("expected Japanese caption URL, got %q", got)
	}
}

func TestExtractCaptionURL_NoCaptions(t *testing.T) {
	page := `<html><body>no captions here</body></html>`
	got := extractCaptionURL(page)
	if got != "" {
		t.Errorf("expected empty string for page without captions, got %q", got)
	}
}

func TestExtractVideoTitle(t *testing.T) {
	page := `<html><head><title>My Cool Video - YouTube</title></head></html>`
	got := extractVideoTitle(page)
	if got != "My Cool Video" {
		t.Errorf("extractVideoTitle = %q, want %q", got, "My Cool Video")
	}
}

func TestUnescapeJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello\nworld`, "hello\nworld"},
		{`foo\u0026bar`, "foo&bar"},
		{`path\/to\/thing`, "path/to/thing"},
		{`say \"hi\"`, `say "hi"`},
	}
	for _, tt := range tests {
		got := unescapeJSON(tt.input)
		if got != tt.want {
			t.Errorf("unescapeJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestReadYouTube_Integration(t *testing.T) {
	// Fake caption XML served by the test server.
	captionXML := `<?xml version="1.0" encoding="utf-8"?>
<transcript>
  <text start="0.0" dur="2.5">Hello and welcome</text>
  <text start="2.5" dur="3.0">to this test video</text>
  <text start="5.5" dur="2.0">about krill facts</text>
</transcript>`

	// Build a fake YouTube page with embedded player response.
	mux := http.NewServeMux()
	mux.HandleFunc("/watch", func(w http.ResponseWriter, r *http.Request) {
		// The caption URL points back to our test server.
		captionBase := "http://" + r.Host + "/api/timedtext?v=abc123\\u0026lang=en"
		page := `<html><head><title>Krill Facts 101 - YouTube</title></head><body>
		<script>var ytInitialPlayerResponse = {"captions":{"playerCaptionsTracklistRenderer":{
		"captionTracks":[{"baseUrl":"` + captionBase + `","name":{"simpleText":"English"},"languageCode":"en"}]
		}}};</script></body></html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	})
	mux.HandleFunc("/api/timedtext", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(captionXML))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// ReadYouTube constructs its own URL from the video ID, so we can't
	// directly point it at our test server. Instead, test the internal
	// functions that do the heavy lifting.

	// 1. Test caption URL extraction from the fake page.
	captionBaseExpected := srv.URL + "/api/timedtext?v=abc123&lang=en"
	fakePage := `<html><head><title>Krill Facts 101 - YouTube</title></head><body>
	<script>var ytInitialPlayerResponse = {"captions":{"playerCaptionsTracklistRenderer":{
	"captionTracks":[{"baseUrl":"` + srv.URL + `/api/timedtext?v=abc123\u0026lang=en","name":{"simpleText":"English"},"languageCode":"en"}]
	}}};</script></body></html>`

	captionURL := extractCaptionURL(fakePage)
	if captionURL != captionBaseExpected {
		t.Fatalf("extractCaptionURL = %q, want %q", captionURL, captionBaseExpected)
	}

	// 2. Test title extraction.
	title := extractVideoTitle(fakePage)
	if title != "Krill Facts 101" {
		t.Errorf("extractVideoTitle = %q, want %q", title, "Krill Facts 101")
	}

	// 3. Test caption XML fetching and parsing.
	client := &http.Client{}
	transcript, err := fetchCaptionXML(context.Background(), client, captionURL)
	if err != nil {
		t.Fatalf("fetchCaptionXML error: %v", err)
	}
	if !strings.Contains(transcript, "Hello and welcome") {
		t.Errorf("transcript missing expected text, got: %q", transcript)
	}
	if !strings.Contains(transcript, "krill facts") {
		t.Errorf("transcript missing 'krill facts', got: %q", transcript)
	}
}
