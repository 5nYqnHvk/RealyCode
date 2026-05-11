package responses

import "testing"

func TestTagStripperRemovesScaffoldingAcrossChunks(t *testing.T) {
	var s tagStripper

	parts := []string{
		s.Feed("hello <func"),
		s.Feed("tion_calls>"),
		s.Feed("tool text</function"),
		s.Feed("_calls> world"),
		s.Flush(),
	}

	got := ""
	for _, part := range parts {
		got += part
	}
	if got != "hello tool text world" {
		t.Fatalf("stream output = %q", got)
	}
}

func TestTagStripperPreservesNonScaffoldingAngleBrackets(t *testing.T) {
	var s tagStripper

	got := s.Feed("render <Button<T>> and a < b") + s.Flush()
	want := "render <Button<T>> and a < b"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTagStripperFlushEmitsPartialNonToken(t *testing.T) {
	var s tagStripper

	got := s.Feed("prefix <func")
	if got != "prefix " {
		t.Fatalf("Feed() = %q, want buffered partial token", got)
	}
	got += s.Flush()
	if got != "prefix <func" {
		t.Fatalf("output after Flush() = %q", got)
	}
}

func TestTagStripperResetDropsBufferedPartial(t *testing.T) {
	var s tagStripper

	got := s.Feed("prefix <func")
	if got != "prefix " {
		t.Fatalf("Feed() = %q", got)
	}
	s.Reset()
	got += s.Feed("next") + s.Flush()
	if got != "prefix next" {
		t.Fatalf("output after Reset() = %q", got)
	}
}
