package aiusage

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Claude Code's /usage prints one line per allowance window, for example:
//
//	Current session: 13% used · resets Oct 1 at 3:30pm (Asia/Shanghai)
//	Current week (all models): 4% used · resets Oct 3 at 2pm (Asia/Shanghai)
//	Current week (Opus): 1% used
//
// The text is Claude Code's own contract; a parse that finds no window
// must say so instead of drawing 0%.

var (
	ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

	claudeUsageLineRE = regexp.MustCompile(`^Current (session|week(?: \(([^)]+)\))?):\s*([0-9.]+)% used(?:\s*·\s*resets (.+))?$`)

	// claudeDeniedRE is Claude Code's word that the account, not the
	// reading, is in trouble; such an answer stays visible instead of
	// being replaced by older numbers.
	claudeDeniedRE = regexp.MustCompile(`(?i)\b(401|403)\b|not (logged|signed) in|signed out|sign-in (has )?expired|oauth session expired|could not be refreshed|token (has been )?revoked|unauthorized|forbidden|authentication (failed|required)|invalid (access )?token|(session|usage) limit|hit your limit|using your overages`)
)

// claudeSubscriptionNotice is what a successful /usage run prints when it
// can tell only how the account is billed, with no allowance.
const claudeSubscriptionNotice = "You are currently using your subscription to power your Claude Code usage"

var errClaudeUnavailable = errors.New("Claude Code 的 /usage 暂时没有读数")

// parseClaudeUsage reads /usage's windows; now dates a reset that names no
// year. Windows keep the order Claude Code printed them in: the 5-hour
// session, the unscoped week, then any model-scoped weeks.
func parseClaudeUsage(text string, now time.Time) ([]Window, error) {
	text = strings.TrimSpace(ansiRE.ReplaceAllString(text, ""))
	lines := strings.Split(text, "\n")
	// An account error outranks the numbers on the same screen.
	for _, line := range lines {
		if claudeDeniedRE.MatchString(line) {
			return nil, errors.New("Claude Code: " + strings.TrimSpace(line))
		}
	}
	const week = 7 * 24 * time.Hour
	var out []Window
	for _, line := range lines {
		m := claudeUsageLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		used, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}
		w := Window{Name: "5 hours", UsedPct: used, Length: 5 * time.Hour}
		if m[1] != "session" {
			w.Name, w.Length = "7 days", week
			if scope := strings.TrimSpace(m[2]); scope != "" && !strings.EqualFold(scope, "all models") {
				w.Name, w.Model = "7 days · "+scope, claudeScopeModel(scope)
			}
		}
		if hasWindow(out, w.Name) {
			continue
		}
		if t, ok := claudeResetTime(m[4], now, w.Length); ok {
			w.ResetsAt = t
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		if text == "" || hasSubscriptionNotice(lines) {
			return nil, errClaudeUnavailable
		}
		first, _, _ := strings.Cut(text, "\n")
		return nil, fmt.Errorf("Claude Code 的 /usage 没有读数：%s", clipLine(first))
	}
	return out, nil
}

func hasWindow(ws []Window, name string) bool {
	for _, w := range ws {
		if w.Name == name {
			return true
		}
	}
	return false
}

func hasSubscriptionNotice(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSuffix(strings.TrimSpace(line), ".") == claudeSubscriptionNotice {
			return true
		}
	}
	return false
}

// claudeScopeModel is the word a model-scoped window counts models by, from
// the name Anthropic gives it: "Opus" counts claude-opus-*, "Fable 5.1"
// claude-fable-5-1.
func claudeScopeModel(name string) string {
	return strings.NewReplacer(" ", "-", ".", "-").Replace(strings.ToLower(name))
}

// claudeDeniedStatus says how a denied message reads on the card: which
// sign-in state or limit Claude Code reported. ok is false for a message
// that is not a denial.
func claudeDeniedStatus(msg string) (QuotaStatus, bool) {
	if !claudeDeniedRE.MatchString(msg) {
		return QuotaFailed, false
	}
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "not logged in"), strings.Contains(l, "not signed in"),
		strings.Contains(l, "signed out"):
		return QuotaNotLoggedIn, true
	case strings.Contains(l, "usage limit"), strings.Contains(l, "hit your limit"),
		strings.Contains(l, "overages"):
		return QuotaRateLimited, true
	default:
		return QuotaExpired, true
	}
}

const (
	// claudeResetSlack is how much further ahead than its window a reset
	// may read: this machine's clock may be behind Anthropic's, and Claude
	// Code prints the reset with its seconds dropped.
	claudeResetSlack = 2 * time.Hour
)

// claudeResetTime reads "Oct 1 at 3:30pm (Asia/Shanghai)", "Oct 9, 2:59pm
// (UTC)" and "3pm (Asia/Shanghai)" in the named zone, else local time. It
// ends a window span long, so it is never further ahead than that plus
// claudeResetSlack: a date without a year is in the first year it is less
// than a day past and no further ahead; a bare time is the next one from
// now. Where no reading fits, as for a stale line or a clock that is off,
// the latest already-past reading is used and the window reads as renewed.
// When the clocks go back and read a time twice, the reading not yet past
// is taken.
func claudeResetTime(s string, now time.Time, span time.Duration) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	loc := time.Local
	if i := strings.LastIndex(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
		if l, err := time.LoadLocation(strings.TrimSpace(s[i+1 : len(s)-1])); err == nil {
			loc = l
		}
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "AM", "am"), "PM", "pm")
	ref := now.In(loc)
	latest := ref.Add(span + claudeResetSlack)

	for _, layout := range []string{
		"Jan 2 at 3:04pm", "Jan 2 at 3pm",
		"Jan 2, 2006 at 3:04pm", "Jan 2, 2006 at 3pm",
		"Jan 2, 3:04pm", "Jan 2, 3pm",
		"Jan 2, 2006, 3:04pm", "Jan 2, 2006, 3pm",
	} {
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		if t.Year() != 0 {
			return t, true
		}
		// "Dec 31" read just after New Year is last year's.
		var past time.Time
		for y := ref.Year() - 1; y <= ref.Year()+1; y++ {
			d := time.Date(y, t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
			if r, ok := nextReading(d, ref, ref.Add(-24*time.Hour)); ok && !r.After(latest) {
				return r, true
			}
			if d.Before(ref) {
				past = d
			}
		}
		return past, true
	}

	for _, layout := range []string{"3:04pm", "3pm"} {
		c, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		// A day whose clocks skip that time is passed over: Santiago's
		// skip midnight, so "12:30am" before it is the day after
		// tomorrow's.
		for d := range 3 {
			t := time.Date(ref.Year(), ref.Month(), ref.Day()+d, c.Hour(), c.Minute(), 0, 0, loc)
			if t.Hour() != c.Hour() || t.Minute() != c.Minute() {
				continue
			}
			if r, ok := nextReading(t, ref, ref); ok {
				if !r.After(latest) {
					return r, true
				}
				break
			}
		}
		for d := 0; d >= -2; d-- {
			t := time.Date(ref.Year(), ref.Month(), ref.Day()+d, c.Hour(), c.Minute(), 0, 0, loc)
			if t.Hour() == c.Hour() && t.Minute() == c.Minute() && t.Before(ref) {
				return t, true
			}
		}
		return time.Time{}, false
	}
	return time.Time{}, false
}

// nextReading is the first instant whose clock reads as t's does that isn't
// before ref, else the first that isn't before lim. When the clocks go
// back, an hour is read twice and time.Date may give either instant.
func nextReading(t, ref, lim time.Time) (time.Time, bool) {
	first, last := t, t
	start, end := t.ZoneBounds()
	_, off := t.Zone()
	if !start.IsZero() {
		_, prev := start.Add(-time.Second).Zone()
		if prev > off {
			if e := t.Add(time.Duration(off-prev) * time.Second); e.Before(start) {
				first = e
			}
		}
	}
	if !end.IsZero() {
		_, next := end.Zone()
		if next < off {
			if l := t.Add(time.Duration(off-next) * time.Second); !l.Before(end) {
				last = l
			}
		}
	}
	for _, from := range []time.Time{ref, lim} {
		for _, r := range []time.Time{first, last} {
			if !r.Before(from) {
				return r, true
			}
		}
	}
	return time.Time{}, false
}
