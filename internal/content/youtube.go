package content

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// youtubeIDPattern matches the common YouTube URL formats and extracts the video ID.
var youtubeIDPattern = regexp.MustCompile(`(?:youtube\.com/watch\?.*v=|youtu\.be/|youtube\.com/embed/|youtube\.com/v/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)

// IsYouTubeURL returns true if the URL points to a YouTube video.
func IsYouTubeURL(rawURL string) bool {
	return extractVideoID(rawURL) != ""
}

// extractVideoID pulls the 11-character video ID from a YouTube URL.
func extractVideoID(rawURL string) string {
	if m := youtubeIDPattern.FindStringSubmatch(rawURL); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// ReadYouTube fetches the transcript/captions for a YouTube video and returns
// it as a Document. It works without an API key by scraping the video page for
// caption track metadata, then fetching the timed-text XML.
func ReadYouTube(ctx context.Context, rawURL string) (Document, error) {
	videoID := extractVideoID(rawURL)
	if videoID == "" {
		return Document{}, fmt.Errorf("could not extract YouTube video ID from %q", rawURL)
	}

	pageURL := "https://www.youtube.com/watch?v=" + videoID

	// Fetch the video page to extract caption tracks and title.
	client := newSafeHTTPClient(20 * time.Second)
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return Document{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniKrill/0.1)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return Document{}, fmt.Errorf("fetch YouTube page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Document{}, fmt.Errorf("YouTube returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Document{}, fmt.Errorf("read YouTube page: %w", err)
	}
	page := string(body)

	title := extractVideoTitle(page)
	captionURL := extractCaptionURL(page)
	if captionURL == "" {
		// No captions available - fall back to page metadata summary.
		desc := extractVideoDescription(page)
		if desc == "" && title == "" {
			return Document{}, fmt.Errorf("no captions or metadata found for video %s", videoID)
		}
		text := formatVideoMeta(title, desc, videoID)
		return Document{Source: pageURL, Text: text}, nil
	}

	// Fetch the caption XML.
	transcript, err := fetchCaptionXML(ctx, client, captionURL)
	if err != nil {
		return Document{}, fmt.Errorf("fetch captions: %w", err)
	}

	var b strings.Builder
	if title != "" {
		b.WriteString("Video: " + title + "\n")
		b.WriteString("URL: " + pageURL + "\n\n")
	}
	b.WriteString("Transcript:\n")
	b.WriteString(transcript)

	return Document{Source: pageURL, Text: b.String()}, nil
}

// captionURLRe extracts the first captionTrack baseUrl from the player response JSON
// embedded in the YouTube page. We look for English captions first, then fall back to any.
var captionURLRe = regexp.MustCompile(`"captionTracks"\s*:\s*\[([^\]]+)\]`)
var baseURLRe = regexp.MustCompile(`"baseUrl"\s*:\s*"([^"]+)"`)
var langRe = regexp.MustCompile(`"languageCode"\s*:\s*"([^"]+)"`)

func extractCaptionURL(page string) string {
	m := captionURLRe.FindStringSubmatch(page)
	if len(m) < 2 {
		return ""
	}
	tracks := m[1]

	// Split into individual track objects. This is a heuristic: it assumes
	// },{ never appears inside a JSON string value. In practice YouTube's
	// caption track objects contain only simple key-value pairs, so this
	// holds for real-world responses.
	parts := strings.Split(tracks, "},{")

	// First pass: look for English.
	for _, part := range parts {
		if lm := langRe.FindStringSubmatch(part); len(lm) >= 2 {
			lang := lm[1]
			if strings.HasPrefix(lang, "en") {
				if bm := baseURLRe.FindStringSubmatch(part); len(bm) >= 2 {
					return unescapeJSON(bm[1])
				}
			}
		}
	}

	// Second pass: take the first available track.
	if bm := baseURLRe.FindStringSubmatch(tracks); len(bm) >= 2 {
		return unescapeJSON(bm[1])
	}
	return ""
}

var titleRe = regexp.MustCompile(`<title>([^<]+)</title>`)
var metaTitleRe = regexp.MustCompile(`"title"\s*:\s*"([^"]*)"`)

func extractVideoTitle(page string) string {
	// Try og:title or <title> tag first.
	if m := titleRe.FindStringSubmatch(page); len(m) >= 2 {
		t := strings.TrimSuffix(m[1], " - YouTube")
		return strings.TrimSpace(t)
	}
	if m := metaTitleRe.FindStringSubmatch(page); len(m) >= 2 {
		return unescapeJSON(m[1])
	}
	return ""
}

var descRe = regexp.MustCompile(`"shortDescription"\s*:\s*"([^"]*(?:\\.[^"]*)*)"`)

func extractVideoDescription(page string) string {
	if m := descRe.FindStringSubmatch(page); len(m) >= 2 {
		return unescapeJSON(m[1])
	}
	return ""
}

func formatVideoMeta(title, desc, videoID string) string {
	var b strings.Builder
	if title != "" {
		b.WriteString("Video: " + title + "\n")
	}
	b.WriteString("URL: https://www.youtube.com/watch?v=" + videoID + "\n\n")
	b.WriteString("Note: No captions/transcript available for this video.\n\n")
	if desc != "" {
		b.WriteString("Description:\n" + desc + "\n")
	}
	return b.String()
}

// unescapeJSON decodes a JSON-encoded string value. The input is expected to be
// the content between quotes (not including the quotes themselves) from YouTube's
// inline player response JSON.
func unescapeJSON(s string) string {
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err != nil {
		return s
	}
	return out
}

// timedText is the XML structure YouTube returns for captions.
type timedText struct {
	XMLName xml.Name   `xml:"transcript"`
	Texts   []textNode `xml:"text"`
}

type textNode struct {
	Start string `xml:"start,attr"`
	Dur   string `xml:"dur,attr"`
	Text  string `xml:",chardata"`
}

func fetchCaptionXML(ctx context.Context, client *http.Client, captionURL string) (string, error) {
	// Ensure we request XML format (fmt=3 is timed text XML).
	u, err := url.Parse(captionURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("fmt", "3")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MiniKrill/0.1)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("caption fetch returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}

	var tt timedText
	if err := xml.Unmarshal(body, &tt); err != nil {
		// If XML parsing fails, return raw text extraction as fallback.
		return ExtractText(string(body)), nil
	}

	var b strings.Builder
	for _, t := range tt.Texts {
		line := strings.TrimSpace(t.Text)
		if line == "" {
			continue
		}
		// Unescape HTML entities in caption text.
		line = ExtractText(line)
		b.WriteString(line)
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String()), nil
}
