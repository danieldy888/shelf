package aiusage

import (
	"strings"
	"testing"
	"time"
)

// taipei is the zone the local Claude Code prints its resets in.
func taipei(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Skipf("no zone data: %v", err)
	}
	return loc
}

func TestParseClaudeUsageSessionAndWeek(t *testing.T) {
	loc := taipei(t)
	now := time.Date(2026, 10, 28, 17, 0, 0, 0, loc)
	text := strings.Join([]string{
		"You are currently using your subscription to power your Claude Code usage",
		"",
		"Current session: 3% used · resets Oct 28 at 9:10pm (Asia/Taipei)",
		"Current week (all models): 41% used · resets Oct 31 at 10am (Asia/Taipei)",
	}, "\n")

	ws, err := parseClaudeUsage(text, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("windows = %+v, want session and week", ws)
	}
	if ws[0].Name != "5 hours" || ws[0].Length != 5*time.Hour || ws[0].UsedPct != 3 {
		t.Fatalf("session = %+v", ws[0])
	}
	if want := time.Date(2026, 10, 28, 21, 10, 0, 0, loc); !ws[0].ResetsAt.Equal(want) {
		t.Fatalf("session reset = %v, want %v", ws[0].ResetsAt, want)
	}
	if ws[1].Name != "7 days" || ws[1].Model != "" || ws[1].Length != 7*24*time.Hour || ws[1].UsedPct != 41 {
		t.Fatalf("week = %+v", ws[1])
	}
	if want := time.Date(2026, 10, 31, 10, 0, 0, 0, loc); !ws[1].ResetsAt.Equal(want) {
		t.Fatalf("week reset = %v, want %v", ws[1].ResetsAt, want)
	}
}

func TestParseClaudeUsageScopedWeeks(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	text := strings.Join([]string{
		"Current session: 12% used · resets Oct 28 at 3pm (UTC)",
		"Current week (all models): 41% used · resets Oct 31 at 10am (UTC)",
		"Current week (Opus): 37.5% used · resets Nov 1 at 9am (UTC)",
		"Current week (Fable 5.1): 2% used",
		"Current week (Opus): 99% used", // a duplicate line is ignored
	}, "\n")
	ws, err := parseClaudeUsage(text, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ws) != 4 {
		t.Fatalf("windows = %+v, want 4", ws)
	}
	if ws[2].Name != "7 days · Opus" || ws[2].Model != "opus" || ws[2].UsedPct != 37.5 {
		t.Fatalf("opus = %+v", ws[2])
	}
	if ws[3].Name != "7 days · Fable 5.1" || ws[3].Model != "fable-5-1" {
		t.Fatalf("fable = %+v", ws[3])
	}
	if !ws[3].ResetsAt.IsZero() {
		t.Fatalf("a window with no reset must carry none, got %v", ws[3].ResetsAt)
	}
}

func TestParseClaudeUsageANSIAndNoise(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	text := "\x1b[2mYou are currently using your subscription\x1b[0m\n\x1b[1;32mCurrent session: 5% used\x1b[0m\nnoise"
	ws, err := parseClaudeUsage(text, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ws) != 1 || ws[0].UsedPct != 5 {
		t.Fatalf("windows = %+v", ws)
	}
}

func TestParseClaudeUsageEmptyAndNotice(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for name, text := range map[string]string{
		"empty":    "",
		"spaces":   "  \n\t\n",
		"notice":   "You are currently using your subscription to power your Claude Code usage.",
		"notice 2": "You are currently using your subscription to power your Claude Code usage\n",
	} {
		if _, err := parseClaudeUsage(text, now); err == nil || err.Error() == "" {
			t.Fatalf("%s: expected an explicit error, got %v", name, err)
		}
	}
}

func TestParseClaudeUsageDenied(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, text := range []string{
		"Not logged in · Please run /login",
		"API Error: 401 Unauthorized",
		"You've hit your session limit · resets 3pm",
		"OAuth session expired and could not be refreshed",
	} {
		if _, err := parseClaudeUsage(text, now); err == nil {
			t.Fatalf("%q must be a denial", text)
		}
		if status, ok := claudeDeniedStatus(text); !ok || status == QuotaFailed {
			t.Fatalf("%q: denied status = %v, %v", text, status, ok)
		}
	}
	if _, ok := claudeDeniedStatus("Current session: 5% used"); ok {
		t.Fatal("a normal reading must not read as denied")
	}
}

func TestClaudeResetTimeCrossYear(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, loc)
	got, ok := claudeResetTime("Jan 2 at 3pm (UTC)", now, 7*24*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	if want := time.Date(2027, 1, 2, 15, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("reset = %v, want %v", got, want)
	}

	// Read in the new year, "Dec 31" is last year's and already past.
	now = time.Date(2027, 1, 10, 12, 0, 0, 0, loc)
	got, ok = claudeResetTime("Dec 31 at 10am (UTC)", now, 7*24*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	if !got.Before(now) {
		t.Fatalf("reset = %v should be in the past", got)
	}
}

func TestClaudeResetTimeDaylightSaving(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zone data: %v", err)
	}
	// 2026-11-01 01:30 happens twice: 05:30Z (EDT) and 06:30Z (EST).
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, loc)
	got, ok := claudeResetTime("Nov 1 at 1:30am (America/New_York)", now, 7*24*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	want := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("reset = %v, want the first reading %v", got.UTC(), want)
	}
}

func TestClaudeResetTimeBareTime(t *testing.T) {
	loc := taipei(t)
	now := time.Date(2026, 10, 28, 17, 0, 0, 0, loc)
	got, ok := claudeResetTime("9:10pm (Asia/Taipei)", now, 5*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	if want := time.Date(2026, 10, 28, 21, 10, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("reset = %v, want %v", got, want)
	}
}

func TestClaudeResetTimeHeaderForm(t *testing.T) {
	// Claude Code 2.1.285 prints the comma form.
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	got, ok := claudeResetTime("Oct 9, 2:59pm (UTC)", now, 7*24*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	// A date already past this year is not shifted to next year: the
	// reading is old, so the window reads as renewed.
	if want := time.Date(2026, 10, 9, 14, 59, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("reset = %v, want %v", got, want)
	}
}

func TestClaudeResetTimeSlack(t *testing.T) {
	// A reset further ahead than its window plus slack is not believed:
	// the latest past reading is used instead, so the window reads anew.
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	got, ok := claudeResetTime("Nov 20 at 8pm (UTC)", now, 5*time.Hour)
	if !ok {
		t.Fatal("no reset parsed")
	}
	if got.After(now.Add(5*time.Hour + claudeResetSlack)) {
		t.Fatalf("reset %v is beyond the window's slack", got)
	}
}

func TestElapsedZeroesPassedWindow(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ws := []Window{
		{Name: "5 hours", UsedPct: 88, Length: 5 * time.Hour, ResetsAt: now.Add(-time.Minute)},
		{Name: "7 days", UsedPct: 41, Length: 7 * 24 * time.Hour, ResetsAt: now.Add(time.Hour)},
	}
	got := Elapsed(ws, now)
	if got[0].UsedPct != 0 || !got[0].ResetsAt.IsZero() {
		t.Fatalf("passed window = %+v, want 0 used and no reset", got[0])
	}
	if got[1].UsedPct != 41 || got[1].ResetsAt.IsZero() {
		t.Fatalf("live window = %+v", got[1])
	}
	if ws[0].UsedPct != 88 {
		t.Fatal("Elapsed must not mutate its input")
	}
}

// An account allowance running out is its own state: the session, weekly
// and model-scoped limits are not a lapsed sign-in, and the card must say
// the allowance ran out instead of asking the user to log in again.
func TestClaudeDeniedStatusAccountLimits(t *testing.T) {
	for _, text := range []string{
		"You've hit your session limit · resets 3pm",
		"You've hit your weekly limit · resets Oct 30 at 2pm (Asia/Taipei)",
		"You've hit your Opus limit",
		"Claude AI usage limit reached",
		"You've hit your limit for now",
	} {
		status, ok := claudeDeniedStatus(text)
		if !ok || status != QuotaLimitReached {
			t.Fatalf("%q → status %v, %v; want QuotaLimitReached", text, status, ok)
		}
	}
	// The sign-in messages keep their own states.
	if status, _ := claudeDeniedStatus("Not logged in · Please run /login"); status != QuotaNotLoggedIn {
		t.Fatalf("not logged in → %v", status)
	}
	if status, _ := claudeDeniedStatus("OAuth session expired and could not be refreshed"); status != QuotaExpired {
		t.Fatalf("expired sign-in → %v", status)
	}
}

// A limit message names when the allowance returns; the reset is read off
// it so the card can say so instead of leaving only the percentage of the
// past.
func TestClaudeLimitReset(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	got, ok := claudeLimitReset("You've hit your session limit · resets 3pm", now)
	if !ok {
		t.Fatal("no session reset parsed")
	}
	// A bare time is read in local time, as Claude Code prints it.
	if want := time.Date(2026, 10, 28, 15, 0, 0, 0, time.Local); !got.Equal(want) {
		t.Fatalf("session reset = %v, want %v", got, want)
	}
	got, ok = claudeLimitReset("You've hit your weekly limit · resets Oct 31 at 10am (UTC)", now)
	if !ok {
		t.Fatal("no weekly reset parsed")
	}
	if want := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("weekly reset = %v, want %v", got, want)
	}
	if _, ok := claudeLimitReset("You've hit your limit for now", now); ok {
		t.Fatal("a message without a reset must not invent one")
	}
}
