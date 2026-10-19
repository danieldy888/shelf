package aiusage

import "testing"

func TestParseCodexLine(t *testing.T) {
	pool := map[string]*string{}
	tail := &codexTail{}

	// turn_context sets the model for later records.
	ctx := []byte(`{"timestamp":"2026-10-19T08:00:01.000Z","type":"turn_context","payload":{"model":"gpt-6.1-sol"}}`)
	if _, ok := tail.parseCodexLine(ctx, pool); ok {
		t.Fatal("turn_context must not emit a record")
	}
	if tail.model != "gpt-6.1-sol" {
		t.Fatalf("model = %q", tail.model)
	}

	ev := []byte(`{"timestamp":"2026-10-19T08:00:10.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110},"last_token_usage":{"input_tokens":100,"cached_input_tokens":20,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110}}}}`)
	r, ok := tail.parseCodexLine(ev, pool)
	if !ok {
		t.Fatal("event rejected")
	}
	// input_tokens includes cached: split into Input 80 + CacheRead 20.
	if r.in != 80 || r.cr != 20 || r.out != 10 || r.cw != 0 {
		t.Fatalf("cached split wrong: %+v", r)
	}
	if r.model == nil || *r.model != "gpt-6.1-sol" {
		t.Fatal("model not carried from turn_context")
	}
	if r.priced {
		t.Fatal("codex must be unpriced")
	}

	// Same running total again → duplicate, skipped.
	evDup := []byte(`{"timestamp":"2026-10-19T08:00:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110},"last_token_usage":{"input_tokens":5,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0,"total_tokens":6}}}}`)
	if _, ok := tail.parseCodexLine(evDup, pool); ok {
		t.Fatal("duplicate total_token_usage must be skipped")
	}

	// A different total resumes recording.
	ev2 := []byte(`{"timestamp":"2026-10-19T08:00:12.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"cached_input_tokens":20,"cache_write_input_tokens":0,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":220},"last_token_usage":{"input_tokens":100,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":110}}}}`)
	r2, ok := tail.parseCodexLine(ev2, pool)
	if !ok {
		t.Fatal("next event rejected")
	}
	if r2.in != 100 || r2.cr != 0 || r2.out != 10 {
		t.Fatalf("second rec wrong: %+v", r2)
	}
}

func TestParseCodexLineSkips(t *testing.T) {
	pool := map[string]*string{}
	tail := &codexTail{}
	for name, line := range map[string]string{
		"null-info": `{"timestamp":"2026-10-19T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null}}`,
		"null-last": `{"timestamp":"2026-10-19T08:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0,"total_tokens":2},"last_token_usage":null}}}`,
		"other-ev":  `{"timestamp":"2026-10-19T08:00:00.000Z","type":"event_msg","payload":{"type":"agent_message","token_count":"x"}}`,
		"response":  `{"timestamp":"2026-10-19T08:00:00.000Z","type":"response_item","payload":{"type":"message","token_count":"x"}}`,
		"garbage":   `{"type":"event_msg","token_count`,
		"no-ts":     `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0,"total_tokens":2},"last_token_usage":{"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0,"total_tokens":2}}}}`,
	} {
		if _, ok := tail.parseCodexLine([]byte(line), pool); ok {
			t.Fatalf("%s: should be skipped", name)
		}
	}
}
