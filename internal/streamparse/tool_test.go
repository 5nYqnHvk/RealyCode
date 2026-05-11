package streamparse

import "testing"

func TestHeuristicToolParserFunctionCall(t *testing.T) {
	var p HeuristicToolParser
	safe, calls := p.Feed("before ● <function=Grep><parameter=pattern>test</parameter><parameter=path>internal</parameter> after")
	if safe != "before  after" || len(calls) != 1 || calls[0].Name != "Grep" || calls[0].Input["pattern"] != "test" || calls[0].Input["path"] != "internal" {
		t.Fatalf("safe=%q calls=%+v", safe, calls)
	}
}

func TestHeuristicToolParserStreamingFunctionCall(t *testing.T) {
	var p HeuristicToolParser
	safe, calls := p.Feed("● <function=Read><parameter=file_path>/tmp")
	if safe != "" || len(calls) != 0 {
		t.Fatalf("safe=%q calls=%+v", safe, calls)
	}
	calls = p.Flush()
	if len(calls) != 1 || calls[0].Name != "Read" || calls[0].Input["file_path"] != "/tmp" {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestHeuristicToolParserWebToolJSON(t *testing.T) {
	var p HeuristicToolParser
	safe, calls := p.Feed(`Use WebSearch {"query":"relaycode"}`)
	if safe != "" || len(calls) != 1 || calls[0].Name != "WebSearch" || calls[0].Input["query"] != "relaycode" {
		t.Fatalf("safe=%q calls=%+v", safe, calls)
	}
}
