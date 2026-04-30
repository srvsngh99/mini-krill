package tui

import "testing"

func TestWordWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{
			name:  "empty string",
			text:  "",
			width: 40,
			want:  "",
		},
		{
			name:  "single word within width",
			text:  "hello",
			width: 40,
			want:  "hello",
		},
		{
			name:  "width zero returns original",
			text:  "hello world",
			width: 0,
			want:  "hello world",
		},
		{
			name:  "negative width returns original",
			text:  "hello world",
			width: -1,
			want:  "hello world",
		},
		{
			name:  "short line no wrap needed",
			text:  "hello world",
			width: 40,
			want:  "hello world",
		},
		{
			name:  "wraps at width",
			text:  "one two three four",
			width: 10,
			want:  "one two\n  three\n  four",
		},
		{
			name:  "preserves blank line separators",
			text:  "paragraph one\n\nparagraph two",
			width: 40,
			want:  "paragraph one\n\nparagraph two",
		},
		{
			name:  "preserves list items",
			text:  "- item one\n- item two\n- item three",
			width: 40,
			want:  "- item one\n- item two\n- item three",
		},
		{
			name:  "preserves indentation",
			text:  "  indented line",
			width: 40,
			want:  "  indented line",
		},
		{
			name:  "wraps indented line with continuation",
			text:  "  this is a very long indented line that needs wrapping",
			width: 25,
			want:  "  this is a very long\n    indented line that\n    needs wrapping",
		},
		{
			name:  "multiple paragraphs with lists",
			text:  "Header\n\n- first item\n- second item\n\nFooter",
			width: 40,
			want:  "Header\n\n- first item\n- second item\n\nFooter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wordWrap(tt.text, tt.width)
			if got != tt.want {
				t.Errorf("wordWrap(%q, %d)\n  got:  %q\n  want: %q", tt.text, tt.width, got, tt.want)
			}
		})
	}
}
