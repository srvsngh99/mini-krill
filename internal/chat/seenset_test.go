package chat

import "testing"

func TestSeenSetDedup(t *testing.T) {
	s := newSeenSet(3)
	if !s.take("a") {
		t.Fatal("first take of a should be new")
	}
	if s.take("a") {
		t.Fatal("second take of a should be a duplicate")
	}
}

func TestSeenSetEmptyIDNeverDedups(t *testing.T) {
	s := newSeenSet(3)
	if !s.take("") || !s.take("") {
		t.Fatal("empty id must always be treated as new (never deduped)")
	}
}

func TestSeenSetEviction(t *testing.T) {
	s := newSeenSet(3)
	s.take("a")
	s.take("b")
	s.take("c")
	s.take("d") // exceeds max 3 -> oldest ("a") evicted
	if s.take("b") {
		t.Fatal("b should still be remembered (within window)")
	}
	if !s.take("a") {
		t.Fatal("a should have been evicted and read as new again")
	}
}
