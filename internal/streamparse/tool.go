package streamparse

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

type ToolCall struct {
	ID    string
	Name  string
	Input map[string]string
}

type toolState int

const (
	toolText toolState = iota
	toolMatchingFunction
	toolParsingParameters
)

type HeuristicToolParser struct {
	state  toolState
	buffer string
	id     string
	name   string
	params map[string]string
}

var (
	controlTokenPattern = regexp.MustCompile(`<\|[^|>]{1,80}\|>`)
	funcStartPattern    = regexp.MustCompile(`●\s*<function=([^>]+)>`)
	paramPattern        = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)(?:</parameter>|$)`)
	webToolJSONPattern  = regexp.MustCompile(`(?is)\b(?:use\s+)?(WebFetch|WebSearch)\b.*?(\{.*?\})`)
)

func (p *HeuristicToolParser) Feed(text string) (string, []ToolCall) {
	p.buffer += text
	p.buffer = controlTokenPattern.ReplaceAllString(p.buffer, "")
	if safe, calls := p.extractWebToolJSONCalls(); len(calls) > 0 {
		return safe, calls
	}
	var out strings.Builder
	var calls []ToolCall
	for {
		before := len(p.buffer)
		switch p.state {
		case toolText:
			idx := strings.Index(p.buffer, "●")
			if idx < 0 {
				out.WriteString(p.splitIncompleteControlTokenTail())
				if p.buffer != "" && out.Len() == 0 {
					out.WriteString(p.buffer)
					p.buffer = ""
				}
				return out.String(), calls
			}
			out.WriteString(p.buffer[:idx])
			p.buffer = p.buffer[idx:]
			p.state = toolMatchingFunction
			continue
		case toolMatchingFunction:
			match := funcStartPattern.FindStringSubmatchIndex(p.buffer)
			if match != nil {
				p.name = strings.TrimSpace(p.buffer[match[2]:match[3]])
				p.id = "toolu_heuristic_" + randomID()
				p.params = map[string]string{}
				p.buffer = p.buffer[match[1]:]
				p.state = toolParsingParameters
				continue
			} else if len(p.buffer) > 100 {
				out.WriteByte(p.buffer[0])
				p.buffer = p.buffer[1:]
				p.state = toolText
			} else {
				return out.String(), calls
			}
		case toolParsingParameters:
			finished := false
			for {
				match := paramPattern.FindStringSubmatchIndex(p.buffer)
				if match == nil || !strings.Contains(p.buffer[match[0]:match[1]], "</parameter>") {
					break
				}
				out.WriteString(p.buffer[:match[0]])
				key := strings.TrimSpace(p.buffer[match[2]:match[3]])
				val := strings.TrimSpace(p.buffer[match[4]:match[5]])
				p.params[key] = val
				p.buffer = p.buffer[match[1]:]
			}
			if idx := strings.Index(p.buffer, "●"); idx >= 0 {
				out.WriteString(p.buffer[:idx])
				p.buffer = p.buffer[idx:]
				finished = true
			} else if p.buffer != "" && !strings.HasPrefix(strings.TrimSpace(p.buffer), "<") && !strings.Contains(p.buffer, "<parameter=") {
				out.WriteString(p.buffer)
				p.buffer = ""
				finished = true
			}
			if finished {
				calls = append(calls, ToolCall{ID: p.id, Name: p.name, Input: p.params})
				p.state = toolText
			} else {
				return out.String(), calls
			}
		}
		if len(p.buffer) == before {
			return out.String(), calls
		}
	}
}

func (p *HeuristicToolParser) Flush() []ToolCall {
	p.buffer = controlTokenPattern.ReplaceAllString(p.buffer, "")
	if p.name == "" || (p.state != toolParsingParameters && !strings.Contains(p.buffer, "<parameter=")) {
		return nil
	}
	if p.params == nil {
		p.params = map[string]string{}
	}
	for _, match := range regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*)$`).FindAllStringSubmatch(p.buffer, -1) {
		p.params[strings.TrimSpace(match[1])] = strings.TrimSpace(match[2])
	}
	call := ToolCall{ID: p.id, Name: p.name, Input: p.params}
	p.state = toolText
	p.buffer = ""
	p.name = ""
	return []ToolCall{call}
}

func (p *HeuristicToolParser) extractWebToolJSONCalls() (string, []ToolCall) {
	matches := webToolJSONPattern.FindAllStringSubmatch(p.buffer, -1)
	if len(matches) == 0 {
		return "", nil
	}
	var calls []ToolCall
	for _, match := range matches {
		var input map[string]string
		if err := json.Unmarshal([]byte(match[2]), &input); err != nil {
			continue
		}
		if match[1] == "WebFetch" && input["url"] == "" {
			continue
		}
		if match[1] == "WebSearch" && input["query"] == "" {
			continue
		}
		calls = append(calls, ToolCall{ID: "toolu_heuristic_" + randomID(), Name: match[1], Input: input})
	}
	if len(calls) == 0 {
		return "", nil
	}
	p.buffer = ""
	return "", calls
}

func (p *HeuristicToolParser) splitIncompleteControlTokenTail() string {
	start := strings.LastIndex(p.buffer, "<|")
	if start < 0 || strings.Contains(p.buffer[start:], "|>") {
		emit := p.buffer
		p.buffer = ""
		return emit
	}
	emit := p.buffer[:start]
	p.buffer = p.buffer[start:]
	return emit
}

func randomID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buf[:])
}
