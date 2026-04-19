package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// RegisterBuiltins registers the default set of built-in skills.
func (r *SkillRegistryImpl) RegisterBuiltins() {
	builtins := []core.Skill{
		&recallSkill{},
		&sysinfoSkill{},
		&timeSkill{},
		&webSearchSkill{},
	}
	for _, s := range builtins {
		if err := r.Register(s); err != nil {
			log.Warn("failed to register built-in skill", "name", s.Name(), "error", err)
		}
	}
	log.Debug("built-in skills registered", "count", len(builtins))
}

// RegisterSelfSkills registers all self-awareness and self-modification skills.
func (r *SkillRegistryImpl) RegisterSelfSkills(sc SelfContext) {
	selfSkills := NewSelfSkills(sc)
	for _, s := range selfSkills {
		if err := r.Register(s); err != nil {
			log.Warn("failed to register self-skill", "name", s.Name(), "error", err)
		}
	}
	log.Debug("self-awareness skills registered", "count", len(selfSkills))
}

// ---------------------------------------------------------------------------
// recall skill
// ---------------------------------------------------------------------------

type recallSkill struct{}

func (s *recallSkill) Name() string        { return "recall" }
func (s *recallSkill) Description() string { return "Search the krill's memory for information" }
func (s *recallSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	return input, nil
}

// ---------------------------------------------------------------------------
// sysinfo skill
// ---------------------------------------------------------------------------

type sysinfoSkill struct{}

func (s *sysinfoSkill) Name() string        { return "sysinfo" }
func (s *sysinfoSkill) Description() string { return "Get system information" }

func (s *sysinfoSkill) Execute(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return fmt.Sprintf(
		"System Information:\n"+
			"  OS:           %s\n"+
			"  Architecture: %s\n"+
			"  CPUs:         %d\n"+
			"  Go version:   %s\n"+
			"  Goroutines:   %d\n"+
			"  Memory:\n"+
			"    Allocated:  %s\n"+
			"    System:     %s\n"+
			"    Heap in use: %s\n"+
			"    GC cycles:  %d",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version(),
		runtime.NumGoroutine(),
		formatBytes(mem.TotalAlloc), formatBytes(mem.Sys),
		formatBytes(mem.HeapInuse), mem.NumGC,
	), nil
}

// ---------------------------------------------------------------------------
// time skill
// ---------------------------------------------------------------------------

type timeSkill struct{}

func (s *timeSkill) Name() string        { return "time" }
func (s *timeSkill) Description() string { return "Get current date and time" }

func (s *timeSkill) Execute(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
	now := time.Now()
	return fmt.Sprintf(
		"Current time: %s\nTimezone: %s\nUnix timestamp: %d",
		now.Format("2006-01-02 15:04:05 MST"), now.Location().String(), now.Unix(),
	), nil
}

// ---------------------------------------------------------------------------
// web search skill
// ---------------------------------------------------------------------------

type webSearchSkill struct{}

func (s *webSearchSkill) Name() string        { return "search" }
func (s *webSearchSkill) Description() string { return "Search the web via DuckDuckGo (no API key needed)" }

func (s *webSearchSkill) Execute(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "No search query provided. What should I look up?", nil
	}

	results, err := duckduckgoSearch(ctx, input)
	if err != nil {
		return fmt.Sprintf("Search failed: %v", err), nil
	}
	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %s", input), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %s\n\n", input))
	for i, r := range results {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.title))
		if r.url != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.url))
		}
		if r.snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.snippet))
		}
		sb.WriteString("\n")
	}

	raw := sb.String()

	if llm != nil {
		summary, err := llm.Chat(ctx, []core.Message{
			{Role: "system", Content: "You are a helpful assistant. Summarize these web search results concisely. Include key facts and cite sources."},
			{Role: "user", Content: raw},
		})
		if err == nil && summary.Content != "" {
			return summary.Content, nil
		}
	}

	return raw, nil
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

func duckduckgoSearch(ctx context.Context, query string) ([]searchResult, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + urlEncode(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "MiniKrill/1.0 (search skill)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseSearchResults(string(body)), nil
}

func parseSearchResults(html string) []searchResult {
	var results []searchResult

	chunks := strings.Split(html, "class=\"result__a\"")
	if len(chunks) <= 1 {
		chunks = strings.Split(html, "class='result__a'")
	}

	for i := 1; i < len(chunks) && len(results) < 8; i++ {
		chunk := chunks[i]
		r := searchResult{}

		if hrefIdx := strings.Index(chunk, "href=\""); hrefIdx != -1 {
			start := hrefIdx + 6
			if endIdx := strings.Index(chunk[start:], "\""); endIdx != -1 {
				rawURL := chunk[start : start+endIdx]
				if uddgIdx := strings.Index(rawURL, "uddg="); uddgIdx != -1 {
					rawURL = rawURL[uddgIdx+5:]
					if ampIdx := strings.Index(rawURL, "&"); ampIdx != -1 {
						rawURL = rawURL[:ampIdx]
					}
					rawURL = urlDecode(rawURL)
				}
				r.url = rawURL
			}
		}

		if gtIdx := strings.Index(chunk, ">"); gtIdx != -1 {
			after := chunk[gtIdx+1:]
			if endIdx := strings.Index(after, "</a>"); endIdx != -1 {
				r.title = stripHTML(after[:endIdx])
			}
		}

		if snipIdx := strings.Index(chunk, "result__snippet"); snipIdx != -1 {
			after := chunk[snipIdx:]
			if gtIdx := strings.Index(after, ">"); gtIdx != -1 {
				after = after[gtIdx+1:]
				if endIdx := strings.Index(after, "</"); endIdx != -1 {
					r.snippet = stripHTML(after[:endIdx])
				}
			}
		}

		if r.title != "" {
			results = append(results, r)
		}
	}

	return results
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, c := range s {
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			out.WriteRune(c)
		}
	}
	return strings.TrimSpace(out.String())
}

func urlEncode(s string) string {
	var buf strings.Builder
	for _, c := range s {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			buf.WriteRune(c)
		case c == ' ':
			buf.WriteByte('+')
		case c == '-' || c == '_' || c == '.' || c == '~':
			buf.WriteRune(c)
		default:
			buf.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return buf.String()
}

func urlDecode(s string) string {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if val, err := parseHexByte(s[i+1], s[i+2]); err == nil {
				buf.WriteByte(val)
				i += 2
				continue
			}
		} else if s[i] == '+' {
			buf.WriteByte(' ')
			continue
		}
		buf.WriteByte(s[i])
	}
	return buf.String()
}

func parseHexByte(h1, h2 byte) (byte, error) {
	n1 := hexVal(h1)
	n2 := hexVal(h2)
	if n1 < 0 || n2 < 0 {
		return 0, fmt.Errorf("invalid hex")
	}
	return byte(n1<<4 | n2), nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

func formatBytes(b uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
