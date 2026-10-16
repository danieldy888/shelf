//go:build darwin

package mac

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeNPLine(t *testing.T) {
	full := `{"ok":true,"playing":true,"title":"Title","artist":"Artist","album":"Album",` +
		`"duration":300.5,"elapsed":42.25,"rate":1,"at":1700000000.5,"app":"com.apple.Music",` +
		`"artID":"id-1","art":"` + base64.StdEncoding.EncodeToString([]byte("PNGBYTES")) + `"}`
	u, ok := decodeNPLine(full)
	if !ok {
		t.Fatal("a full helper line must decode")
	}
	s := u.state
	if !s.Playing || s.Title != "Title" || s.Artist != "Artist" || s.Album != "Album" {
		t.Errorf("strings decoded wrong: %+v", s)
	}
	if s.Duration != 300.5 || s.Elapsed != 42.25 || s.Rate != 1 {
		t.Errorf("numbers decoded wrong: %+v", s)
	}
	if s.App != "com.apple.Music" || s.ArtID != "id-1" {
		t.Errorf("ids decoded wrong: %+v", s)
	}
	if string(s.Art) != "PNGBYTES" || s.ArtMissing {
		t.Errorf("artwork decoded wrong: %+v", s)
	}
	if want := time.Unix(1700000000, 500000000); !s.Received.Equal(want) {
		t.Errorf("at: got %v, want %v", s.Received, want)
	}
}

func TestDecodeNPLineVariants(t *testing.T) {
	cases := []struct {
		name string
		line string
		ok   bool
		err  string
	}{
		{"error line", `{"error":"bad command"}`, true, "bad command"},
		{"empty object", `{}`, true, ""},
		{"missing fields default", `{"playing":false}`, true, ""},
		{"unknown fields are ignored", `{"future":"x","playing":true}`, true, ""},
		{"not json", `hello`, false, ""},
		{"json array", `[1,2]`, false, ""},
		{"json null", `null`, false, ""},
		{"empty line", ``, false, ""},
		{"truncated", `{"playing":`, false, ""},
		{"string not object", `"hello"`, false, ""},
		{"number not object", `42`, false, ""},
	}
	for _, c := range cases {
		u, ok := decodeNPLine(c.line)
		if ok != c.ok {
			t.Errorf("%s: decode(%q) ok = %v, want %v", c.name, c.line, ok, c.ok)
			continue
		}
		if ok && u.err != c.err {
			t.Errorf("%s: err = %q, want %q", c.name, u.err, c.err)
		}
	}
}

func TestDecodeNPLineBadArtworkIsEmptyNotFatal(t *testing.T) {
	u, ok := decodeNPLine(`{"playing":true,"artID":"x","art":"!!not base64!!"}`)
	if !ok {
		t.Fatal("line must still decode")
	}
	if u.state.Art != nil {
		t.Errorf("bad base64 must leave Art nil, got %d bytes", len(u.state.Art))
	}
	if u.state.ArtID != "x" {
		t.Errorf("artID must survive a bad art blob, got %q", u.state.ArtID)
	}
}

func TestPositionExtrapolatesWhilePlaying(t *testing.T) {
	recv := time.Unix(1000, 0)
	playing := NowPlayingState{Playing: true, Elapsed: 10, Rate: 1, Duration: 300, Received: recv}
	if got := playing.Position(recv.Add(2500 * time.Millisecond)); got != 12.5 {
		t.Errorf("playing position = %v, want 12.5", got)
	}
	paused := NowPlayingState{Playing: false, Elapsed: 10, Rate: 1, Duration: 300, Received: recv}
	if got := paused.Position(recv.Add(time.Hour)); got != 10 {
		t.Errorf("paused position = %v, want 10 (frozen)", got)
	}
	// Never past the end, never negative.
	over := NowPlayingState{Playing: true, Elapsed: 299, Rate: 1, Duration: 300, Received: recv}
	if got := over.Position(recv.Add(time.Minute)); got != 300 {
		t.Errorf("position past the end = %v, want 300", got)
	}
	neg := NowPlayingState{Playing: false, Elapsed: -5, Received: recv}
	if got := neg.Position(recv); got != 0 {
		t.Errorf("negative position = %v, want 0", got)
	}
	// A stream (no duration) still advances.
	live := NowPlayingState{Playing: true, Elapsed: 1, Rate: 1, Received: recv}
	if got := live.Position(recv.Add(4 * time.Second)); got != 5 {
		t.Errorf("position without duration = %v, want 5", got)
	}
}

func TestFormatClock(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "0:00"},
		{9.4, "0:09"},
		{59.5, "1:00"},
		{60, "1:00"},
		{61, "1:01"},
		{599, "9:59"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{3661, "1:01:01"},
		{-3, "0:00"},
	}
	for _, c := range cases {
		if got := FormatClock(c.sec); got != c.want {
			t.Errorf("FormatClock(%v) = %q, want %q", c.sec, got, c.want)
		}
	}
}

// fakeHostScript writes a small shell script that speaks just enough of
// the helper protocol: it prints one state line, echoes commands, and
// quits on demand. Tests exercise the host's plumbing with it instead of
// perl, so they run anywhere.
func fakeHostScript(t *testing.T, state string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fakehelper")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '" + state + "'\n" +
		"while IFS= read -r line; do\n" +
		"  printf 'echo:%s\\n' \"$line\"\n" +
		"  [ \"$line\" = quit ] && exit 0\n" +
		"done\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNowPlayingHostReadsAndSends(t *testing.T) {
	state := `{"ok":true,"playing":true,"title":"T","artist":"A","duration":100,"elapsed":5}`
	path := fakeHostScript(t, state)
	h := NewNowPlayingHost()
	h.start = func(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
		return startCommand(ctx, path)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, ok := h.State()
		if ok {
			if !st.Playing || st.Title != "T" || st.Artist != "A" || st.Duration != 100 || st.Elapsed != 5 {
				t.Fatalf("state decoded wrong: %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no state arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.Send(NPPlayPause)
	// The fake echoes commands; the state stays the last one parsed, and
	// the echo lines are not states, so the host must stay alive.
	time.Sleep(50 * time.Millisecond)
	if h.Dead() {
		t.Fatal("host died on a command echo")
	}
	h.Send(NPNext)
	h.Send(NPPrevious)
	time.Sleep(50 * time.Millisecond)
	if h.Dead() {
		t.Fatal("host died after commands")
	}
}

func TestNowPlayingHostDeadAfterHelperExit(t *testing.T) {
	path := fakeHostScript(t, `{"ok":true,"playing":false}`)
	h := NewNowPlayingHost()
	h.start = func(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
		return startCommand(ctx, path)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	// Closing our side of stdin makes the fake exit; the host must notice.
	if in, ok := h.cmd.(io.Closer); ok {
		in.Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for !h.Dead() {
		if time.Now().After(deadline) {
			t.Fatal("host did not notice the helper exit")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNowPlayingHostStartFailureIsReported(t *testing.T) {
	h := NewNowPlayingHost()
	h.start = func(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
		return nil, nil, os.ErrNotExist
	}
	if err := h.Start(); err == nil {
		t.Fatal("Start must report the failure")
	}
	if !h.Dead() {
		t.Fatal("a host that never started must read as dead")
	}
	h.Stop() // must be safe on a host that never started
}

func TestCommandName(t *testing.T) {
	cases := map[NowPlayingCommand]string{
		NPPlayPause: "toggle",
		NPNext:      "next",
		NPPrevious:  "prev",
	}
	for cmd, want := range cases {
		if got := commandName(cmd); got != want {
			t.Errorf("commandName(%d) = %q, want %q", cmd, got, want)
		}
	}
	if got := commandName(NowPlayingCommand(99)); got != "" {
		t.Errorf("unknown command must map to the empty string, got %q", got)
	}
}

// TestLiveNowPlayingProbe reports what the system is playing right now,
// read through the Apple-signed perl host. It is read-only (no commands)
// and only runs with SHELF_NP_PROBE=1, since it starts a real process and
// reflects the user's live media state.
func TestLiveNowPlayingProbe(t *testing.T) {
	if os.Getenv("SHELF_NP_PROBE") != "1" {
		t.Skip("set SHELF_NP_PROBE=1 to probe the live Now Playing state")
	}
	bundle, err := helperBundlePath()
	if err != nil {
		t.Skipf("helper bundle not built (run go generate ./internal/mac/nowplaying): %v", err)
	}
	t.Logf("helper bundle: %s", bundle)
	h := NewNowPlayingHost()
	if err := h.Start(); err != nil {
		t.Fatalf("start perl host: %v", err)
	}
	defer h.Stop()
	deadline := time.Now().Add(5 * time.Second)
	var st NowPlayingState
	for {
		if s, ok := h.State(); ok {
			st = s
			break
		}
		if h.Dead() {
			t.Fatal("host exited before reporting state")
		}
		if time.Now().After(deadline) {
			t.Fatal("no state line within 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("playing=%v app=%q title=%q artist=%q album=%q duration=%.1f elapsed=%.1f rate=%.2f artwork=%d bytes artID=%q artMissing=%v",
		st.Playing, st.App, st.Title, st.Artist, st.Album, st.Duration, st.Elapsed, st.Rate,
		len(st.Art), st.ArtID, st.ArtMissing)
	if st.Playing && st.Title == "" {
		t.Logf("note: a playing source reported no title (protected or metadata-less source)")
	}
}

// startCommand starts path with its stdin/stdout pipes, the same shape
// startPerlHost returns.
func startCommand(ctx context.Context, path string) (io.WriteCloser, io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, path)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return in, out, nil
}

func TestDecodeNPLineRoundTripFromHelperSource(t *testing.T) {
	// Guard against the C emitter's exact field names drifting: every
	// field the Go decoder reads must appear in helper.c.
	src, err := os.ReadFile("nowplaying/helper.c")
	if err != nil {
		t.Skipf("helper source not found: %v", err)
	}
	for _, field := range []string{"playing", "title", "artist", "album", "duration",
		"elapsed", "rate", "at", "app", "artID", "artMissing", "art", "error"} {
		if !strings.Contains(string(src), `"`+field+`\"`) && !strings.Contains(string(src), `\"`+field) {
			t.Errorf("helper.c does not mention field %q", field)
		}
	}
}
