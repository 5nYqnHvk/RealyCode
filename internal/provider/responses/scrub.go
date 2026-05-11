package responses

import "strings"

// scaffoldingTokens are suppressed entirely from streamed text. Some
// OpenAI-compatible gateways leak their internal tool-call scaffolding
// into text deltas alongside the real function_call output item.
//
// Only exact matches on this list are stripped; genuine angle-bracket text
// (JSX, C++ templates, comparisons) passes through untouched.
var scaffoldingTokens = []string{
	"<function_calls>",
	"</function_calls>",
	"<function_call>",
	"</function_call>",
}

// longestScaffoldingLen is the max token byte length (for buffer bounds).
var longestScaffoldingLen = func() int {
	n := 0
	for _, t := range scaffoldingTokens {
		if len(t) > n {
			n = len(t)
		}
	}
	return n
}()

// tagStripper removes only the exact scaffoldingTokens strings from a stream,
// even when they are split across deltas. Anything else — including unrelated
// tag-shaped tokens like `<Button>` — passes through unchanged.
type tagStripper struct {
	buf strings.Builder
}

func (t *tagStripper) Feed(chunk string) string {
	if chunk == "" {
		return ""
	}
	t.buf.WriteString(chunk)
	return t.drain(false)
}

func (t *tagStripper) Flush() string {
	if t.buf.Len() == 0 {
		return ""
	}
	return t.drain(true)
}

// Reset discards any buffered partial token. Call between separate output_text
// items so orphan scaffolding fragments (e.g. lone `<` from a split message)
// do not leak when their item closes.
func (t *tagStripper) Reset() {
	t.buf.Reset()
}

func (t *tagStripper) drain(final bool) string {
	s := t.buf.String()
	t.buf.Reset()

	var out strings.Builder
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			out.WriteString(s[i:])
			i = len(s)
			break
		}
		out.WriteString(s[i : i+lt])
		start := i + lt
		remaining := s[start:]

		// Exact scaffolding token match: drop.
		matched := false
		for _, tok := range scaffoldingTokens {
			if strings.HasPrefix(remaining, tok) {
				i = start + len(tok)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		// Could this `<...` still complete into a scaffolding token?
		// Only buffer if remaining is a proper prefix of one (and we have room).
		if !final && isScaffoldingPrefix(remaining) && len(remaining) < longestScaffoldingLen {
			t.buf.WriteString(remaining)
			return out.String()
		}

		// Not scaffolding: emit `<` as literal text and continue scanning.
		out.WriteByte('<')
		i = start + 1
	}
	return out.String()
}

func isScaffoldingPrefix(s string) bool {
	for _, tok := range scaffoldingTokens {
		if len(s) < len(tok) && strings.HasPrefix(tok, s) {
			return true
		}
	}
	return false
}
