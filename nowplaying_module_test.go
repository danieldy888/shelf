//go:build darwin

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// stubNowPlaying builds a module whose helper is a pair of pipes: the
// test writes helper lines into it and reads the commands sent back, so
// no perl and no real MediaRemote are involved.
type stubNowPlaying struct {
	mod   *nowPlayingModule
	lines io.WriteCloser // test → module: helper stdout
	cmds  chan string    // module → test: commands the module sent
}

func newStubNowPlaying(t *testing.T, on bool) *stubNowPlaying {
	t.Helper()
	// module → test (commands the module sends)
	cmdR, cmdW := io.Pipe()
	// test → module (helper stdout lines)
	lineR, lineW := io.Pipe()
	s := &stubNowPlaying{lines: lineW, cmds: make(chan string, 8)}
	// Drain the command pipe on its own goroutine: a click handler writes
	// synchronously, so a full pipe must never block the UI thread.
	go func() {
		sc := bufio.NewScanner(cmdR)
		for sc.Scan() {
			s.cmds <- sc.Text()
		}
		close(s.cmds)
	}()
	host := mac.NewNowPlayingHostWithSource(func(ctx context.Context) (io.WriteCloser, io.ReadCloser, error) {
		// The stub child "exits" when the host is stopped, so Stop does
		// not wait out its one-second grace period.
		go func() {
			<-ctx.Done()
			lineR.Close()
		}()
		return cmdW, lineR, nil
	})
	s.mod = &nowPlayingModule{
		host: &shelf{settings: settings.Settings{IslandNowPlaying: on}},
	}
	s.mod.np = host
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	s.mod.started = true
	t.Cleanup(func() {
		host.Stop()
		lineW.Close()
		cmdW.Close()
	})
	return s
}

// feed pushes one helper line and steps refreshes until the module has
// folded it in (the helper runs on its own goroutine, so the line is
// never there instantly).
func (s *stubNowPlaying) feed(t *testing.T, line string) mac.NowPlayingState {
	t.Helper()
	s.mod.refresh(time.Now())
	before := s.mod.snap
	if _, err := io.WriteString(s.lines, line+"\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mod.refresh(time.Now())
		if s.mod.snap != before {
			return s.mod.last
		}
		if time.Now().After(deadline) {
			t.Fatalf("line %s did not change the module state", line)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestNPSnapOf(t *testing.T) {
	now := time.Now()
	if got := npSnapOf(mac.NowPlayingState{}, false, now); got != (npSnap{}) {
		t.Fatalf("no state must project to the zero snapshot, got %+v", got)
	}
	st := mac.NowPlayingState{Playing: true, Title: "T", Artist: "A", ArtID: "id",
		Elapsed: 10, Rate: 1, Received: now.Add(-2 * time.Second)}
	got := npSnapOf(st, true, now)
	want := npSnap{have: true, playing: true, title: "T", artist: "A", artID: "id", position: 12}
	if got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	// A paused track keeps its frozen position but reports not playing.
	paused := npSnapOf(mac.NowPlayingState{Title: "T", Elapsed: 10, Received: now.Add(-time.Hour)}, true, now)
	if paused.playing || paused.position != 10 {
		t.Fatalf("paused snapshot = %+v", paused)
	}
}

func TestNowPlayingActivityOnlyWhilePlayingAndEnabled(t *testing.T) {
	s := newStubNowPlaying(t, true)
	if _, live := s.mod.activity(time.Now()); live {
		t.Fatal("no state yet: the island must stay empty")
	}
	s.feed(t, `{"ok":true,"playing":true,"title":"T","artist":"A","duration":100,"elapsed":5}`)
	act, live := s.mod.activity(time.Now())
	if !live {
		t.Fatal("playing state must produce an island activity")
	}
	if act.ID != "nowplaying" || act.Priority != nowPlayingPriority || act.Level != -1 || act.Trailing != "" {
		t.Fatalf("activity shape: %+v", act)
	}
	if act.Leading != ui.ImageSource(musicSymbol) {
		t.Fatalf("without artwork the leading glyph is the music icon, got %T", act.Leading)
	}
	if act.LeadingTint != textPrimary {
		t.Fatalf("the icon carries the text colour, got %v", act.LeadingTint)
	}
	// Paused: the island folds away, the card stays for resuming.
	s.feed(t, `{"ok":true,"playing":false,"title":"T","artist":"A","duration":100,"elapsed":5}`)
	if _, live := s.mod.activity(time.Now()); live {
		t.Fatal("paused playback must not hold the island")
	}
	if _, ok := s.mod.track(); !ok {
		t.Fatal("paused playback must still have a card")
	}
	// Turning the setting off hides both.
	s.mod.host.settings.IslandNowPlaying = false
	s.mod.tick(time.Now())
	if _, live := s.mod.activity(time.Now()); live {
		t.Fatal("the setting off must hide the island")
	}
	if _, ok := s.mod.track(); ok {
		t.Fatal("the setting off must hide the card")
	}
	if s.mod.np != nil || s.mod.started {
		t.Fatal("the setting off must stop the helper")
	}
}

func TestNowPlayingCardDrawsTrackAndControls(t *testing.T) {
	s := newStubNowPlaying(t, true)
	s.feed(t, `{"ok":true,"playing":true,"title":"夜曲","artist":"周杰伦","duration":100,"elapsed":5}`)
	a := &shelf{settings: settings.Settings{IslandNowPlaying: true}}
	a.modules = []module{s.mod, &retentionModule{host: a}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("夜曲") || !view.HasText("周杰伦") {
		t.Fatal("the card must show title and artist")
	}
	if !view.HasText("0:05") {
		t.Fatal("the card must show the elapsed position")
	}
	// Clicking the play/pause button must send the toggle command to the
	// helper; the button is reachable by its accessibility label.
	if err := view.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	s.expectCommand(t, "toggle")
}

func TestNowPlayingCardHiddenWithoutTrack(t *testing.T) {
	s := newStubNowPlaying(t, true)
	if s.mod.card(nil) {
		t.Fatal("no track: the card must not claim it drew anything")
	}
	// A helper line with nothing playing is not a track either.
	s.feed(t, `{"ok":true,"playing":false,"title":"","artist":""}`)
	if s.mod.card(nil) {
		t.Fatal("an empty state must not draw a card")
	}
}

func TestNowPlayingDecodeArtOncePerTrack(t *testing.T) {
	m := &nowPlayingModule{}
	pngBytes := testPNG(t)
	m.decodeArt(mac.NowPlayingState{ArtID: "a", Art: pngBytes})
	if m.art == nil {
		t.Fatal("a valid PNG must decode")
	}
	if m.artTried != "a" {
		t.Fatalf("artTried = %q, want a", m.artTried)
	}
	// The same track never re-decodes.
	bitmap := m.art
	m.decodeArt(mac.NowPlayingState{ArtID: "a", Art: pngBytes})
	if m.art != bitmap {
		t.Fatal("the same track must reuse the decoded bitmap")
	}
	// A new track with a broken blob drops the old artwork and is not
	// retried.
	m.decodeArt(mac.NowPlayingState{ArtID: "b", Art: []byte("not a png")})
	if m.art != nil || m.artTried != "b" {
		t.Fatalf("a broken blob must clear the art and be remembered: art=%v tried=%q", m.art, m.artTried)
	}
	// A track the helper marked as artwork-less is remembered too.
	m.decodeArt(mac.NowPlayingState{ArtID: "c", ArtMissing: true})
	if m.artTried != "c" {
		t.Fatalf("artTried = %q, want c", m.artTried)
	}
	// Artwork that arrives late for a track still decodes (not marked
	// tried by a plain empty line).
	m.decodeArt(mac.NowPlayingState{ArtID: "d"})
	if m.artTried == "d" {
		t.Fatal("an empty artwork without artMissing must not be remembered as tried")
	}
	m.decodeArt(mac.NowPlayingState{ArtID: "d", Art: pngBytes})
	if m.art == nil {
		t.Fatal("late artwork must still decode")
	}
}

func TestNowPlayingStopsWhenHelperDies(t *testing.T) {
	s := newStubNowPlaying(t, true)
	s.feed(t, `{"ok":true,"playing":true,"title":"T","elapsed":1}`)
	if !s.mod.playing() {
		t.Fatal("state must be live")
	}
	// The helper's stdout ends: the module must notice, drop the state
	// and schedule a restart rather than showing a frozen track.
	if err := s.lines.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !s.mod.np.Dead() {
		if time.Now().After(deadline) {
			t.Fatal("host did not notice the helper exit")
		}
		time.Sleep(2 * time.Millisecond)
	}
	s.mod.tick(time.Now())
	if s.mod.playing() || s.mod.np != nil || s.mod.started {
		t.Fatal("a dead helper must clear the state and stop the module")
	}
	// The restart is delayed, not immediate (no process churn).
	if s.mod.retryAt.IsZero() {
		t.Fatal("a restart must be scheduled with a delay")
	}
}

func TestNowPlayingStartFailureIsRetriedLater(t *testing.T) {
	m := &nowPlayingModule{host: &shelf{settings: settings.Settings{IslandNowPlaying: true}}}
	m.newHost = func() *mac.NowPlayingHost {
		return mac.NewNowPlayingHostWithSource(func(context.Context) (io.WriteCloser, io.ReadCloser, error) {
			return nil, nil, errors.New("no helper here")
		})
	}
	now := time.Now()
	m.tick(now)
	if m.started || m.np != nil {
		t.Fatal("a failed start must not leave a host behind")
	}
	if m.retryAt.IsZero() || !m.retryAt.After(now) {
		t.Fatal("a failed start must schedule a retry")
	}
	// The next ticks inside the delay must not even try again.
	tries := 0
	m.newHost = func() *mac.NowPlayingHost {
		tries++
		return mac.NewNowPlayingHostWithSource(func(context.Context) (io.WriteCloser, io.ReadCloser, error) {
			return nil, nil, errors.New("still nothing")
		})
	}
	m.tick(now.Add(npRetryDelay / 2))
	if tries != 0 {
		t.Fatal("a tick inside the retry delay must not restart the helper")
	}
	m.tick(now.Add(npRetryDelay + time.Second))
	if tries != 1 {
		t.Fatal("a tick past the delay must retry once")
	}
	if _, live := m.activity(now); live {
		t.Fatal("a module that never started must stay silent")
	}
}

func TestPlayPauseAffordances(t *testing.T) {
	if toggleSymbol(true) != pauseSymbol2 || toggleSymbol(false) != playSymbol {
		t.Fatal("the toggle glyph must match the playing state")
	}
	if playPauseLabel(true) != "暂停" || playPauseLabel(false) != "播放" {
		t.Fatal("the toggle label must name the action it will take")
	}
}

// expectCommand checks the next command the module sent to the helper.
func (s *stubNowPlaying) expectCommand(t *testing.T, want string) {
	t.Helper()
	select {
	case got, ok := <-s.cmds:
		if !ok {
			t.Fatalf("the command pipe closed before %q arrived", want)
		}
		if got != want {
			t.Fatalf("the module sent %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no %q command arrived", want)
	}
}

// testPNG is a tiny valid PNG for the artwork decode path.
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, color.RGBA{R: 200, G: 30, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
