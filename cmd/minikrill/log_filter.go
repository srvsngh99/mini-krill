package main

import (
	"bytes"
	"io"
	"regexp"
)

// noiseFilter wraps an io.Writer and drops well-known macOS dyld diagnostic
// lines that have no signal value. Without it, dive.log fills up with
// "minikrill(NNNN) MallocStackLogging: can't turn off malloc stack logging
// because it was not enabled." for every subprocess spawn.
//
// The filter operates line-by-line. Bytes received without a trailing newline
// are buffered until the next write so we never split a line mid-pattern.
type noiseFilter struct {
	w   io.Writer
	buf bytes.Buffer
}

func newNoiseFilter(w io.Writer) *noiseFilter {
	return &noiseFilter{w: w}
}

// dyldNoisePatterns lists regexes that match log lines we always want to drop.
// Add sparingly — anything matched here is silently discarded.
var dyldNoisePatterns = []*regexp.Regexp{
	regexp.MustCompile(`MallocStackLogging:`),
	regexp.MustCompile(`^objc\[\d+\]: Class .* implemented in both`),
}

func (f *noiseFilter) Write(p []byte) (int, error) {
	original := len(p)
	f.buf.Write(p)

	for {
		idx := bytes.IndexByte(f.buf.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := f.buf.Next(idx + 1) // includes '\n'
		if !isNoise(line) {
			if _, err := f.w.Write(line); err != nil {
				return original, err
			}
		}
	}
	return original, nil
}

// Flush emits any bytes still buffered after the last newline. The daemon
// process tends to end every line with `\n`, so this is mostly a contract
// nicety — but if a child exits mid-line (panic, segfault) without a
// trailing newline, those bytes would otherwise vanish silently.
func (f *noiseFilter) Flush() error {
	if f.buf.Len() == 0 {
		return nil
	}
	leftover := f.buf.Bytes()
	if isNoise(leftover) {
		f.buf.Reset()
		return nil
	}
	_, err := f.w.Write(leftover)
	f.buf.Reset()
	return err
}

func isNoise(line []byte) bool {
	for _, pat := range dyldNoisePatterns {
		if pat.Match(line) {
			return true
		}
	}
	return false
}
