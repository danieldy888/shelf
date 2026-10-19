package aiusage

import "testing"

func TestParseOmpLine(t *testing.T) {
	pool := map[string]*string{}
	line := []byte(`{"type":"message","id":"e1","timestamp":"2026-10-19T10:00:01.000Z","message":{"role":"assistant","provider":"p","model":"m1","usage":{"input":10,"output":3,"cacheRead":7,"cacheWrite":2,"cost":{"total":0.5}},"timestamp":1792404001000}}`)
	r, key, ok := parseOmpLine(line, pool)
	if !ok {
		t.Fatal("line rejected")
	}
	if r.at != 1792404001000 || r.in != 10 || r.out != 3 || r.cr != 7 || r.cw != 2 {
		t.Fatalf("bad rec: %+v", r)
	}
	if !r.priced || r.cost != 0.5 {
		t.Fatalf("bad price: %+v", r)
	}
	if r.model == nil || *r.model != "m1" {
		t.Fatalf("bad model: %v", r.model)
	}
	if key == "" {
		t.Fatal("no dedupe key")
	}
}

func TestParseOmpLineFallbacks(t *testing.T) {
	pool := map[string]*string{}
	// No message.timestamp → entry ISO timestamp; no cost → unpriced.
	line := []byte(`{"type":"message","id":"e2","timestamp":"2026-10-19T10:00:01.000Z","message":{"role":"assistant","provider":"prov","usage":{"input":1,"output":1}}}`)
	r, key, ok := parseOmpLine(line, pool)
	if !ok {
		t.Fatal("line rejected")
	}
	if r.at != 1792404001000 {
		t.Fatalf("at = %d, want entry timestamp", r.at)
	}
	if r.priced || r.cost != 0 {
		t.Fatalf("want unpriced: %+v", r)
	}
	if r.model == nil || *r.model != "prov" {
		t.Fatalf("model should fall back to provider: %v", r.model)
	}
	if key == "" {
		t.Fatal("no dedupe key")
	}
}

func TestParseOmpLineSkips(t *testing.T) {
	pool := map[string]*string{}
	for name, line := range map[string]string{
		"no-usage":      `{"type":"message","id":"e3","message":{"role":"assistant","timestamp":1792404001000}}`,
		"user-role":     `{"type":"message","id":"e4","message":{"role":"user","usage":{"input":1},"timestamp":1792404001000}}`,
		"not-message":   `{"type":"title","id":"e5","usage":{"input":1}}`,
		"garbage":       `{"type":"message","id":"e6","message":{"role":"assistant","usage":{"input":`,
		"no-timestamp":  `{"type":"message","id":"e7","message":{"role":"assistant","usage":{"input":1}}}`,
		"not-assistant": `{"type":"message","id":"e8","message":{"role":"tool","usage":{"input":1},"timestamp":1792404001000}}`,
	} {
		if _, _, ok := parseOmpLine([]byte(line), pool); ok {
			t.Fatalf("%s: should be skipped", name)
		}
	}
}

func TestParseMillis(t *testing.T) {
	cases := map[string]int64{
		`1792404001000`:              1792404001000,
		`"2026-10-19T10:00:01.000Z"`: 1792404001000,
		`1792404001`:                 1792404001000, // epoch seconds
		`0`:                          0,
		`"garbage"`:                  0,
		`null`:                       0,
		``:                           0,
	}
	for in, want := range cases {
		if got := parseMillis([]byte(in)); got != want {
			t.Fatalf("parseMillis(%q) = %d, want %d", in, got, want)
		}
	}
}
