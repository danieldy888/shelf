package aiusage

import "testing"

func TestParseClaudeLine(t *testing.T) {
	pool := map[string]*string{}
	line := []byte(`{"type":"assistant","uuid":"u1","timestamp":"2026-10-19T10:00:05.000Z","message":{"id":"msg_1","model":"claude-sonnet-4-5","role":"assistant","usage":{"input_tokens":500,"output_tokens":100,"cache_creation_input_tokens":40,"cache_read_input_tokens":900}}}`)
	r, key, ok := parseClaudeLine(line, pool)
	if !ok {
		t.Fatal("line rejected")
	}
	if r.in != 500 || r.out != 100 || r.cr != 900 || r.cw != 40 {
		t.Fatalf("bad rec: %+v", r)
	}
	if r.priced || r.cost != 0 {
		t.Fatalf("claude must be unpriced: %+v", r)
	}
	if key != "msg_1" {
		t.Fatalf("key = %q, want message id", key)
	}
	if r.model == nil || *r.model != "claude-sonnet-4-5" {
		t.Fatal("bad model")
	}
}

func TestParseClaudeLineSkips(t *testing.T) {
	pool := map[string]*string{}
	for name, line := range map[string]string{
		"synthetic":  `{"type":"assistant","uuid":"u2","timestamp":"2026-10-19T10:00:00.000Z","message":{"id":"m","model":"<synthetic>","usage":{"input_tokens":1,"output_tokens":1}}}`,
		"user":       `{"type":"user","uuid":"u3","timestamp":"2026-10-19T10:00:00.000Z","message":{"role":"user","usage":{"input_tokens":1}}}`,
		"no-usage":   `{"type":"assistant","uuid":"u4","timestamp":"2026-10-19T10:00:00.000Z","message":{"id":"m","model":"x"}}`,
		"garbage":    `{"type":"assistant","uuid":"u5","usage":`,
		"no-ts":      `{"type":"assistant","uuid":"u6","message":{"id":"m","model":"x","usage":{"input_tokens":1}}}`,
		"null-usage": `{"type":"assistant","uuid":"u7","timestamp":"2026-10-19T10:00:00.000Z","message":{"id":"m","model":"x","usage":null}}`,
	} {
		if _, _, ok := parseClaudeLine([]byte(line), pool); ok {
			t.Fatalf("%s: should be skipped", name)
		}
	}
}

func TestParseClaudeLineUUIDFallbackKey(t *testing.T) {
	pool := map[string]*string{}
	line := []byte(`{"type":"assistant","uuid":"u9","timestamp":"2026-10-19T10:00:05.000Z","message":{"model":"x","usage":{"input_tokens":1,"output_tokens":1}}}`)
	r, key, ok := parseClaudeLine([]byte(line), pool)
	if !ok {
		t.Fatal("line rejected")
	}
	_ = r
	if key == "" || key[0] != 'u' {
		t.Fatalf("want uuid fallback key, got %q", key)
	}
}
