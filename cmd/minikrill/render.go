package main

import "strings"

// renderMarkdown converts common markdown patterns to ANSI-colored terminal output.
func renderMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	inCodeBlock := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			out = append(out, cDim+"  "+strings.Repeat("-", 40)+cReset)
			continue
		}
		if inCodeBlock {
			out = append(out, cDimCyan+"  "+line+cReset)
			continue
		}

		trimmed := strings.TrimSpace(line)

		// Headers
		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, cBCyan+strings.TrimPrefix(trimmed, "### ")+cReset)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, cBCyan+strings.TrimPrefix(trimmed, "## ")+cReset)
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, cBold+cCyan+strings.TrimPrefix(trimmed, "# ")+cReset)
			continue
		}

		// Bullet lists
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			out = append(out, cCyan+"  -"+cReset+" "+renderInline(trimmed[2:]))
			continue
		}

		// Numbered lists
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed[:3], ".") {
			dotIdx := strings.Index(trimmed, ".")
			if dotIdx > 0 && dotIdx < 4 {
				num := trimmed[:dotIdx]
				rest := strings.TrimSpace(trimmed[dotIdx+1:])
				out = append(out, cCyan+"  "+num+"."+cReset+" "+renderInline(rest))
				continue
			}
		}

		out = append(out, renderInline(line))
	}

	return strings.Join(out, "\n")
}

// renderInline handles inline markdown: **bold**, *italic*, `code`
func renderInline(s string) string {
	// Bold: **text**
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		inner := s[start+2 : end]
		s = s[:start] + cBold + inner + cReset + s[end+2:]
	}

	// Italic: *text* (but not **)
	for {
		start := -1
		for i := 0; i < len(s); i++ {
			if s[i] == '*' && (i+1 >= len(s) || s[i+1] != '*') && (i == 0 || s[i-1] != '*') {
				start = i
				break
			}
		}
		if start == -1 {
			break
		}
		end := -1
		for i := start + 1; i < len(s); i++ {
			if s[i] == '*' && (i+1 >= len(s) || s[i+1] != '*') && s[i-1] != '*' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		inner := s[start+1 : end]
		s = s[:start] + cDim + inner + cReset + s[end+1:]
	}

	// Inline code: `text`
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		inner := s[start+1 : end]
		s = s[:start] + cDimCyan + inner + cReset + s[end+1:]
	}

	return s
}
