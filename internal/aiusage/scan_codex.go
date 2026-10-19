package aiusage

import (
	"bytes"
	"encoding/json"
)

// Codex session entries: ~/.codex/sessions/**/rollout-*.jsonl and
// ~/.codex/archived_sessions/**/*.jsonl
//
// Relevant lines:
//   {"timestamp":"2026-10-03T16:29:05.637Z","type":"event_msg",
//    "payload":{"type":"token_count",
//      "info":{"total_token_usage":{"input_tokens":38915,
//        "cached_input_tokens":22144,"cache_write_input_tokens":0,
//        "output_tokens":144,"reasoning_output_tokens":0,"total_tokens":39059},
//              "last_token_usage":{…}, "model_context_window":258400},
//      "rate_limits":{…}}}
//   {"timestamp":"…","type":"turn_context","payload":{"model":"gpt-6.1-sol",…}}
//
// last_token_usage is the delta of one step; total_token_usage the running
// total. input_tokens *includes* cached_input_tokens, so Input is stored
// without the cached part and CacheRead carries it. A token_count event
// whose total equals the previous event's is a duplicate write and is
// skipped. info == null skips too. Codex records no price.

type codexUsageJSON struct {
	Input       int64 `json:"input_tokens"`
	CachedInput int64 `json:"cached_input_tokens"`
	CacheWrite  int64 `json:"cache_write_input_tokens"`
	Output      int64 `json:"output_tokens"`
	Reasoning   int64 `json:"reasoning_output_tokens"`
	Total       int64 `json:"total_tokens"`
}

type codexEntryJSON struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   struct {
		Type  string `json:"type"`
		Model string `json:"model"` // turn_context
		Info  *struct {
			Total codexUsageJSON  `json:"total_token_usage"`
			Last  *codexUsageJSON `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// parseCodexLine decodes one line, updating the file's resume state
// (current model, previous total signature). ok=false skips the line.
func (t *codexTail) parseCodexLine(line []byte, pool map[string]*string) (r rec, ok bool) {
	if !bytes.Contains(line, []byte(`token_count`)) && !bytes.Contains(line, []byte(`turn_context`)) {
		return r, false
	}
	var e codexEntryJSON
	if json.Unmarshal(line, &e) != nil {
		return r, false
	}
	p := &e.Payload
	if e.Type == "turn_context" {
		if p.Model != "" {
			t.model = p.Model
		}
		return r, false
	}
	if e.Type != "event_msg" || p.Type != "token_count" || p.Info == nil {
		return r, false
	}
	tot := p.Info.Total
	if t.havePrev && t.prev == [6]int64{tot.Input, tot.CachedInput, tot.CacheWrite, tot.Output, tot.Reasoning, tot.Total} {
		return r, false // same running total re-logged: duplicate
	}
	t.prev = [6]int64{tot.Input, tot.CachedInput, tot.CacheWrite, tot.Output, tot.Reasoning, tot.Total}
	t.havePrev = true
	u := p.Info.Last
	if u == nil {
		return r, false
	}
	at := parseMillis(e.Timestamp)
	if at == 0 {
		return r, false
	}
	r.at = at
	r.model = intern(pool, t.model)
	in := u.Input - u.CachedInput
	if in < 0 {
		in = 0
	}
	r.in, r.cr, r.cw, r.out = in, u.CachedInput, u.CacheWrite, u.Output
	return r, true
}
