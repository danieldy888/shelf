package aiusage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// claudeCredentials is the JSON Claude Code stores in the login Keychain
// (service "Claude Code-credentials") and mirrors into
// ~/.claude/.credentials.json on some setups.
type claudeCredentials struct {
	OAuth struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"` // Unix milliseconds
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// claudeUsage is GET {base}/api/oauth/usage.
type claudeUsage struct {
	FiveHour *claudeWindow `json:"five_hour"`
	SevenDay *claudeWindow `json:"seven_day"`
}

type claudeWindow struct {
	// utilization absent must not read as 0% used: presence is part of
	// the value.
	Utilization *float64 `json:"utilization"` // 0–100
	ResetsAt    string   `json:"resets_at"`   // ISO 8601
}

const claudeKeychainService = "Claude Code-credentials"

const (
	claude5hLen = 5 * time.Hour
	claude7dLen = 7 * 24 * time.Hour
)

// FetchClaude reads Claude Code's OAuth credentials (login Keychain first,
// then the credentials file) and asks Anthropic for the subscription's usage
// windows. An expired token returns QuotaExpired without any request — ShelfU
// never refreshes tokens, because refreshing would rotate Claude Code's own
// session and could sign it out. FetchedAt is stamped when the response
// arrives so a Retry-After delay counts from receipt; ctx cancellation
// short-circuits before any credential read.
func FetchClaude(ctx context.Context, env Env) Quota {
	q := Quota{Tool: ToolClaude, FetchedAt: env.now()}

	if err := ctx.Err(); err != nil {
		q.Status = QuotaFailed
		q.Err = shortErr(err)
		return q
	}
	cred, err := readClaudeCredentials(ctx, env)
	if err != nil {
		if errors.Is(err, errNotLoggedIn) {
			q.Status = QuotaNotLoggedIn
		} else {
			q.Status = QuotaFailed
			q.Err = shortErr(err)
		}
		return q
	}
	q.Plan = prettyPlan(cred.OAuth.SubscriptionType)
	if cred.OAuth.AccessToken == "" {
		q.Status = QuotaNotLoggedIn
		return q
	}
	if cred.OAuth.ExpiresAt > 0 && !q.FetchedAt.Before(time.UnixMilli(cred.OAuth.ExpiresAt)) {
		q.Status = QuotaExpired
		return q
	}
	if err := ctx.Err(); err != nil {
		q.Status = QuotaFailed
		q.Err = shortErr(err)
		return q
	}

	res, err := env.get(ctx, env.claudeBase()+"/api/oauth/usage", [][2]string{
		{"Authorization", "Bearer " + cred.OAuth.AccessToken},
		{"anthropic-beta", "oauth-2025-04-20"},
		{"User-Agent", "claude-code/2.1.0"},
		{"Accept", "application/json"},
	})
	q.FetchedAt = env.now() // response received (or the request failed) now
	if finishStatus(&q, res, err) {
		return q
	}

	var usage claudeUsage
	if err := decodeJSON(res.body, &usage); err != nil {
		q.Status = QuotaFailed
		q.Err = shortErr(err)
		return q
	}
	q.FiveHour = usage.FiveHour.window(claude5hLen)
	q.Weekly = usage.SevenDay.window(claude7dLen)
	q.Status = QuotaOK
	return q
}

func (w *claudeWindow) window(length time.Duration) *Window {
	if w == nil || w.Utilization == nil {
		return nil
	}
	win := &Window{UsedPct: *w.Utilization, Length: length}
	if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
		win.ResetsAt = t
	}
	return win
}

// readClaudeCredentials tries the login Keychain first (Claude Code's
// canonical store), then ~/.claude/.credentials.json. A credential JSON that
// lacks claudeAiOauth counts as absent, and a file read error other than
// "missing" is a real failure.
func readClaudeCredentials(ctx context.Context, env Env) (claudeCredentials, error) {
	var cred claudeCredentials
	if env.Keychain != nil {
		if blob, err := env.Keychain(ctx, claudeKeychainService); err == nil {
			if json.Unmarshal(blob, &cred) == nil && cred.OAuth.AccessToken != "" {
				return cred, nil
			}
		}
	}
	// A cancel during (or just after) the Keychain read ends the fetch:
	// falling through to the file would keep reading credentials after the
	// caller asked to stop.
	if err := ctx.Err(); err != nil {
		return claudeCredentials{}, err
	}
	data, err := readFile(joinHome(env.Home, ".claude", ".credentials.json"))
	if err != nil {
		return cred, err
	}
	if err := json.Unmarshal(data, &cred); err != nil || cred.OAuth.AccessToken == "" {
		return claudeCredentials{}, errNotLoggedIn
	}
	return cred, nil
}
