package aiusage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// omp session entries: ~/.omp/agent/sessions/**.jsonl
//
// Relevant lines look like:
//   {"type":"message","id":"011bd63b","parentId":"…","timestamp":"…",
//    "message":{"role":"assistant","provider":"…","model":"…",
//               "usage":{"input":0,"output":30123,"cacheRead":0,
//                        "cacheWrite":0,"totalTokens":30123,
//                        "cost":{…,"total":0}},
//               "timestamp":1789137363486}}
//
// message.timestamp is milliseconds since epoch; the entry timestamp is an
// ISO 8601 string (fallback). Only assistant messages carry usage.
//
// Dedupe: forked/resumed sessions copy prior history into new files, so a
// record is keyed by entry id + message timestamp + usage numbers. On this
// machine's real corpus that key drops the 8 copied records and no
// legitimate one (entry ids are unique within a file).

type ompUsageJSON struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Cost       *struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

type ompEntryJSON struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Timestamp json.RawMessage `json:"timestamp"`
	Message   struct {
		Role      string          `json:"role"`
		Provider  string          `json:"provider"`
		Model     string          `json:"model"`
		Timestamp json.RawMessage `json:"timestamp"`
		Usage     *ompUsageJSON   `json:"usage"`
	} `json:"message"`
}

// parseOmpLine decodes one line; ok=false skips it (not an assistant usage
// entry, malformed, or no usable timestamp).
func parseOmpLine(line []byte, pool map[string]*string) (r rec, key string, ok bool) {
	if !bytes.Contains(line, []byte(`"usage"`)) {
		return r, "", false
	}
	var e ompEntryJSON
	if json.Unmarshal(line, &e) != nil {
		return r, "", false
	}
	u := e.Message.Usage
	if e.Type != "message" || e.Message.Role != "assistant" || u == nil {
		return r, "", false
	}
	at := parseMillis(e.Message.Timestamp)
	if at == 0 {
		at = parseMillis(e.Timestamp)
	}
	if at == 0 {
		return r, "", false
	}
	r.at = at
	r.model = intern(pool, e.Message.Model)
	if r.model == nil {
		r.model = intern(pool, e.Message.Provider)
	}
	r.in, r.out, r.cr, r.cw = u.Input, u.Output, u.CacheRead, u.CacheWrite
	if u.Cost != nil {
		r.cost = u.Cost.Total
		r.priced = true // the provider reported a price (even $0)
	}
	key = e.ID + "|" + strconv.FormatInt(at, 10) + "|" +
		strconv.FormatInt(u.Input, 10) + "," + strconv.FormatInt(u.Output, 10) + "," +
		strconv.FormatInt(u.CacheRead, 10) + "," + strconv.FormatInt(u.CacheWrite, 10)
	return r, key, true
}

// parseMillis reads a timestamp that is either epoch milliseconds (number)
// or an RFC 3339 string. 0 = no usable timestamp.
func parseMillis(raw json.RawMessage) int64 {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return 0
	}
	if b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil {
			return 0
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			if t2, err2 := time.Parse("2006-01-02T15:04:05Z0700", s); err2 == nil {
				return t2.UnixMilli()
			}
			return 0
		}
		return t.UnixMilli()
	}
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return 0
	}
	if f >= 1e9 && f < 1e11 {
		return int64(f * 1000) // looks like epoch seconds
	}
	if f < 1e11 {
		return 0
	}
	return int64(f)
}
