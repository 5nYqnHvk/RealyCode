package streamparse

import "testing"

func TestThinkTagParserBasic(t *testing.T) {
	var p ThinkTagParser
	chunks := p.Feed("Hello <think>reasoning</think> world")
	if len(chunks) != 3 || chunks[0].Kind != TextChunk || chunks[0].Content != "Hello " || chunks[1].Kind != ThinkingChunk || chunks[1].Content != "reasoning" || chunks[2].Content != " world" {
		t.Fatalf("chunks = %+v", chunks)
	}
}

func TestThinkTagParserStreaming(t *testing.T) {
	var p ThinkTagParser
	chunks := append(p.Feed("Hello <thi"), p.Feed("nk>reason")...)
	chunks = append(chunks, p.Feed("ing</think> done")...)
	var text, thinking string
	for _, chunk := range chunks {
		if chunk.Kind == ThinkingChunk {
			thinking += chunk.Content
		} else {
			text += chunk.Content
		}
	}
	if text != "Hello  done" || thinking != "reasoning" {
		t.Fatalf("chunks = %+v", chunks)
	}
}

func TestThinkTagParserFlushInside(t *testing.T) {
	var p ThinkTagParser
	chunks := p.Feed("<think>partial")
	flushed := p.Flush()
	if len(chunks) != 1 || chunks[0].Kind != ThinkingChunk || chunks[0].Content != "partial" || flushed != nil {
		t.Fatalf("chunks=%+v flushed=%+v", chunks, flushed)
	}
}
