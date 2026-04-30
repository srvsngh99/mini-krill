package reminder

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	inRe       = regexp.MustCompile(`(?i)\bin\s+(\d+)\s*(minute|minutes|min|hour|hours|day|days)\b`)
	tomorrowRe = regexp.MustCompile(`(?i)\btomorrow(?:\s+at)?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
)

func Parse(text, at string, now time.Time) (string, time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", time.Time{}, fmt.Errorf("reminder text is empty")
	}
	if strings.TrimSpace(at) != "" {
		due, err := parseAt(at, now)
		return text, due, err
	}
	if m := inRe.FindStringSubmatch(text); len(m) == 3 {
		n, _ := strconv.Atoi(m[1])
		unit := strings.ToLower(m[2])
		d := time.Duration(n) * time.Minute
		switch {
		case strings.HasPrefix(unit, "hour"):
			d = time.Duration(n) * time.Hour
		case strings.HasPrefix(unit, "day"):
			d = time.Duration(n) * 24 * time.Hour
		}
		clean := strings.TrimSpace(inRe.ReplaceAllString(text, ""))
		return clean, now.Add(d), nil
	}
	if m := tomorrowRe.FindStringSubmatch(text); len(m) >= 2 {
		hour, _ := strconv.Atoi(m[1])
		minute := 0
		if len(m) > 2 && m[2] != "" {
			minute, _ = strconv.Atoi(m[2])
		}
		if len(m) > 3 {
			ampm := strings.ToLower(m[3])
			if ampm == "pm" && hour < 12 {
				hour += 12
			}
			if ampm == "am" && hour == 12 {
				hour = 0
			}
		}
		due := time.Date(now.Year(), now.Month(), now.Day()+1, hour, minute, 0, 0, now.Location())
		clean := strings.TrimSpace(tomorrowRe.ReplaceAllString(text, ""))
		return clean, due, nil
	}
	return text, time.Time{}, fmt.Errorf("could not parse due time; use --at RFC3339 or phrases like 'in 10 minutes' / 'tomorrow 9am'")
}

func parseAt(at string, now time.Time) (time.Time, error) {
	at = strings.TrimSpace(at)
	if d, err := time.ParseDuration(at); err == nil {
		return now.Add(d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02 3:04pm", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, at, now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse --at %q", at)
}
