package streamparse

import "strings"

type ChunkKind int

const (
	TextChunk ChunkKind = iota
	ThinkingChunk
)

type Chunk struct {
	Kind    ChunkKind
	Content string
}

type ThinkTagParser struct {
	buffer string
	inTag  bool
}

const openThinkTag = "<think>"
const closeThinkTag = "</think>"

func (p *ThinkTagParser) Feed(content string) []Chunk {
	p.buffer += content
	var chunks []Chunk
	for p.buffer != "" {
		before := len(p.buffer)
		var chunk *Chunk
		if p.inTag {
			chunk = p.parseInside()
		} else {
			chunk = p.parseOutside()
		}
		if chunk != nil && chunk.Content != "" {
			chunks = append(chunks, *chunk)
		}
		if len(p.buffer) == before {
			break
		}
	}
	return chunks
}

func (p *ThinkTagParser) Flush() *Chunk {
	if p.buffer == "" {
		return nil
	}
	kind := TextChunk
	if p.inTag {
		kind = ThinkingChunk
	}
	chunk := Chunk{Kind: kind, Content: p.buffer}
	p.buffer = ""
	return &chunk
}

func (p *ThinkTagParser) parseOutside() *Chunk {
	start := strings.Index(p.buffer, openThinkTag)
	orphanClose := strings.Index(p.buffer, closeThinkTag)
	if orphanClose >= 0 && (start < 0 || orphanClose < start) {
		pre := p.buffer[:orphanClose]
		p.buffer = p.buffer[orphanClose+len(closeThinkTag):]
		return &Chunk{Kind: TextChunk, Content: pre}
	}
	if start < 0 {
		if idx := p.partialTagStart(); idx >= 0 {
			emit := p.buffer[:idx]
			p.buffer = p.buffer[idx:]
			return &Chunk{Kind: TextChunk, Content: emit}
		}
		emit := p.buffer
		p.buffer = ""
		return &Chunk{Kind: TextChunk, Content: emit}
	}
	pre := p.buffer[:start]
	p.buffer = p.buffer[start+len(openThinkTag):]
	p.inTag = true
	return &Chunk{Kind: TextChunk, Content: pre}
}

func (p *ThinkTagParser) parseInside() *Chunk {
	end := strings.Index(p.buffer, closeThinkTag)
	if end < 0 {
		last := strings.LastIndex(p.buffer, "<")
		if last >= 0 && len(p.buffer)-last < len(closeThinkTag) && strings.HasPrefix(closeThinkTag, p.buffer[last:]) {
			emit := p.buffer[:last]
			p.buffer = p.buffer[last:]
			return &Chunk{Kind: ThinkingChunk, Content: emit}
		}
		emit := p.buffer
		p.buffer = ""
		return &Chunk{Kind: ThinkingChunk, Content: emit}
	}
	emit := p.buffer[:end]
	p.buffer = p.buffer[end+len(closeThinkTag):]
	p.inTag = false
	return &Chunk{Kind: ThinkingChunk, Content: emit}
}

func (p *ThinkTagParser) partialTagStart() int {
	idx := strings.LastIndex(p.buffer, "<")
	if idx < 0 {
		return -1
	}
	tail := p.buffer[idx:]
	if len(tail) < len(openThinkTag) && strings.HasPrefix(openThinkTag, tail) {
		return idx
	}
	if len(tail) < len(closeThinkTag) && strings.HasPrefix(closeThinkTag, tail) {
		return idx
	}
	return -1
}
