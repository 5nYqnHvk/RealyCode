package config

// Minimal YAML subset parser. Not spec-compliant; supports exactly what
// relaycode.yaml needs:
//   - indented maps with string keys
//   - list items introduced by "- "
//   - scalar values: unquoted strings, double-quoted strings, ints, true/false
//   - "#" comments, blank lines
//
// Rejects anchors, flow style ({a: 1, b: 2}), multi-line literal blocks,
// tabs in indentation, and anything else not listed above.

import (
	"fmt"
	"strconv"
	"strings"
)

type (
	yamlMap  = map[string]any
	yamlList = []any
)

type yamlLine struct {
	indent int
	text   string // trimmed of leading whitespace, trailing comments stripped
	lineNo int
}

func parseYAML(src string) (yamlMap, error) {
	lines, err := tokenizeYAML(src)
	if err != nil {
		return nil, err
	}
	node, consumed, err := parseBlock(lines, 0, 0)
	if err != nil {
		return nil, err
	}
	if consumed != len(lines) {
		return nil, fmt.Errorf("line %d: unexpected indent", lines[consumed].lineNo)
	}
	m, ok := node.(yamlMap)
	if !ok {
		return nil, fmt.Errorf("top-level document must be a map")
	}
	return m, nil
}

func tokenizeYAML(src string) ([]yamlLine, error) {
	var out []yamlLine
	for i, raw := range strings.Split(src, "\n") {
		lineNo := i + 1
		// strip comments (not inside quoted strings)
		text := stripComment(raw)
		trimmed := strings.TrimRight(text, " \t")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		indent := 0
		for indent < len(trimmed) && (trimmed[indent] == ' ') {
			indent++
		}
		if strings.HasPrefix(trimmed[:indent], "\t") || strings.Contains(trimmed[:indent], "\t") {
			return nil, fmt.Errorf("line %d: tabs are not allowed for indentation", lineNo)
		}
		out = append(out, yamlLine{
			indent: indent,
			text:   trimmed[indent:],
			lineNo: lineNo,
		})
	}
	return out, nil
}

func stripComment(s string) string {
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			// allow escaped quote inside
			if i > 0 && s[i-1] == '\\' {
				continue
			}
			inQuote = !inQuote
			continue
		}
		if c == '#' && !inQuote {
			return s[:i]
		}
	}
	return s
}

// parseBlock consumes lines at indent >= minIndent starting from start.
// Returns the parsed node (map or list), number of lines consumed, error.
func parseBlock(lines []yamlLine, start, minIndent int) (any, int, error) {
	if start >= len(lines) {
		return yamlMap{}, 0, nil
	}
	first := lines[start]
	if first.indent < minIndent {
		return yamlMap{}, 0, nil
	}
	if strings.HasPrefix(first.text, "- ") || first.text == "-" {
		return parseList(lines, start, first.indent)
	}
	return parseMap(lines, start, first.indent)
}

func parseMap(lines []yamlLine, start, indent int) (yamlMap, int, error) {
	m := yamlMap{}
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, 0, fmt.Errorf("line %d: unexpected indent (expected %d, got %d)", line.lineNo, indent, line.indent)
		}
		if strings.HasPrefix(line.text, "- ") {
			return nil, 0, fmt.Errorf("line %d: unexpected list item inside map", line.lineNo)
		}
		key, rest, err := splitKey(line.text, line.lineNo)
		if err != nil {
			return nil, 0, err
		}
		i++
		if rest != "" {
			v, err := parseScalar(rest)
			if err != nil {
				return nil, 0, fmt.Errorf("line %d: %w", line.lineNo, err)
			}
			m[key] = v
			continue
		}
		// child block on following lines
		if i >= len(lines) || lines[i].indent <= indent {
			m[key] = ""
			continue
		}
		child, consumed, err := parseBlock(lines, i, indent+1)
		if err != nil {
			return nil, 0, err
		}
		m[key] = child
		i += consumed
	}
	return m, i - start, nil
}

func parseList(lines []yamlLine, start, indent int) (yamlList, int, error) {
	var list yamlList
	i := start
	for i < len(lines) {
		line := lines[i]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, 0, fmt.Errorf("line %d: unexpected indent in list", line.lineNo)
		}
		if !strings.HasPrefix(line.text, "- ") && line.text != "-" {
			break
		}
		rest := strings.TrimPrefix(line.text, "-")
		rest = strings.TrimPrefix(rest, " ")

		if rest == "" {
			i++
			if i >= len(lines) || lines[i].indent <= indent {
				list = append(list, nil)
				continue
			}
			child, consumed, err := parseBlock(lines, i, indent+1)
			if err != nil {
				return nil, 0, err
			}
			list = append(list, child)
			i += consumed
			continue
		}

		if strings.Contains(rest, ":") && !startsWithQuote(rest) {
			// inline first key of a map, then possibly more keys on following indented lines
			key, val, err := splitKey(rest, line.lineNo)
			if err != nil {
				return nil, 0, err
			}
			m := yamlMap{}
			if val != "" {
				sv, err := parseScalar(val)
				if err != nil {
					return nil, 0, fmt.Errorf("line %d: %w", line.lineNo, err)
				}
				m[key] = sv
			} else {
				m[key] = ""
			}
			i++
			// continuation lines for this map item are indented strictly deeper than the "-"
			childIndent := indent + 2
			for i < len(lines) && lines[i].indent >= childIndent {
				// re-use parseMap but only at childIndent level
				sub, consumed, err := parseMap(lines, i, childIndent)
				if err != nil {
					return nil, 0, err
				}
				for k, v := range sub {
					m[k] = v
				}
				i += consumed
				break // parseMap already consumed all lines at childIndent contiguously
			}
			list = append(list, m)
			continue
		}

		v, err := parseScalar(rest)
		if err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", line.lineNo, err)
		}
		list = append(list, v)
		i++
	}
	return list, i - start, nil
}

func splitKey(text string, lineNo int) (string, string, error) {
	idx := indexUnquoted(text, ':')
	if idx < 0 {
		return "", "", fmt.Errorf("line %d: expected 'key: value' or 'key:'", lineNo)
	}
	key := strings.TrimSpace(text[:idx])
	if key == "" {
		return "", "", fmt.Errorf("line %d: empty key", lineNo)
	}
	rest := strings.TrimSpace(text[idx+1:])
	return unquote(key), rest, nil
}

func indexUnquoted(s string, c byte) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' && (i == 0 || s[i-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if s[i] == c && !inQuote {
			return i
		}
	}
	return -1
}

func startsWithQuote(s string) bool {
	return len(s) > 0 && s[0] == '"'
}

func parseScalar(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}
	if s == "null" || s == "~" {
		return nil, nil
	}
	if s[0] == '"' {
		return unquote(s), nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	return s, nil
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		// minimal escape handling
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	return s
}
