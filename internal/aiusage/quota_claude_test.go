package aiusage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// keychainFixture returns a Keychain func serving the named fixture.
func keychainFixture(t *testing.T, name string) func(context.Context, string) ([]byte, error) {
	t.Helper()
	return func(ctx context.Context, service string) ([]byte, error) {
		if service != claudeKeychainService {
			return nil, fmt.Errorf("unexpected service %q", service)
		}
		return os.ReadFile(filepath.Join("testdata", "quota", name))
	}
}

func keychainMissing() func(context.Context, string) ([]byte, error) {
	return func(context.Context, string) ([]byte, error) {
		return nil, errors.New("The specified item could not be found in the keychain.")
	}
}

// claudeServer records headers and replies with status/body.
func claudeServer(t *testing.T, status int, body string, headers map[string]string) (*httptest.Server, *int) {
	t.Helper()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func claudeEnv(srv *httptest.Server, home string, kc func(context.Context, string) ([]byte, error)) Env {
	return Env{
		Home:          home,
		HTTP:          srv.Client(),
		Keychain:      kc,
		ClaudeBaseURL: srv.URL,
		Now:           func() time.Time { return fakeNow },
	}
}

// writeClaudeFile drops a credentials file under home/.claude/.
func writeClaudeFile(t *testing.T, home, fixtureName string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "quota", fixtureName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFetchClaudeOK(t *testing.T) {
	srv, hits := claudeServer(t, 200, fixture(t, "claude_usage.json"), nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaOK {
		t.Fatalf("status = %v, err %q", q.Status, q.Err)
	}
	if q.Tool != ToolClaude {
		t.Fatalf("tool = %v", q.Tool)
	}
	if q.Plan != "Pro" {
		t.Fatalf("plan = %q", q.Plan)
	}
	if q.FiveHour == nil || q.Weekly == nil {
		t.Fatal("both windows should be set")
	}
	if q.FiveHour.UsedPct != 42 || q.FiveHour.Length != 5*time.Hour {
		t.Fatalf("five-hour = %+v", q.FiveHour)
	}
	if q.Weekly.UsedPct != 18 || q.Weekly.Length != 7*24*time.Hour {
		t.Fatalf("weekly = %+v", q.Weekly)
	}
	want, _ := time.Parse(time.RFC3339, "2027-01-10T15:00:00.123456Z")
	if !q.FiveHour.ResetsAt.Equal(want) {
		t.Fatalf("five-hour resets at %v, want %v", q.FiveHour.ResetsAt, want)
	}
	if *hits != 1 {
		t.Fatalf("server hits = %d", *hits)
	}
}

func TestFetchClaudeHeaders(t *testing.T) {
	var gotAuth, gotBeta, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fixture(t, "claude_usage.json"))
	}))
	defer srv.Close()
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	FetchClaude(context.Background(), env)
	if gotAuth != "Bearer sk-ant-oat01-testtoken" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Fatalf("anthropic-beta = %q", gotBeta)
	}
	if gotUA != "claude-code/2.1.0" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
}

func TestFetchClaudeFileFallback(t *testing.T) {
	// Keychain misses; the credentials file supplies the token.
	home := t.TempDir()
	writeClaudeFile(t, home, "claude_credentials_max.json")
	srv, hits := claudeServer(t, 200, fixture(t, "claude_usage.json"), nil)
	env := claudeEnv(srv, home, keychainMissing())

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaOK {
		t.Fatalf("status = %v, err %q", q.Status, q.Err)
	}
	if q.Plan != "Max" {
		t.Fatalf("plan = %q", q.Plan)
	}
	if *hits != 1 {
		t.Fatalf("server hits = %d", *hits)
	}
}

func TestFetchClaudeKeychainGarbageFallsBack(t *testing.T) {
	home := t.TempDir()
	writeClaudeFile(t, home, "claude_credentials.json")
	kc := func(context.Context, string) ([]byte, error) {
		return []byte("not json"), nil
	}
	srv, _ := claudeServer(t, 200, fixture(t, "claude_usage.json"), nil)
	env := claudeEnv(srv, home, kc)

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaOK {
		t.Fatalf("status = %v", q.Status)
	}
}

func TestFetchClaudeNotLoggedIn(t *testing.T) {
	srv, hits := claudeServer(t, 200, "{}", nil)
	env := claudeEnv(srv, t.TempDir(), keychainMissing())

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaNotLoggedIn {
		t.Fatalf("status = %v", q.Status)
	}
	if *hits != 0 {
		t.Fatalf("expected no request, got %d", *hits)
	}
}

func TestFetchClaudeExpired(t *testing.T) {
	srv, hits := claudeServer(t, 200, "{}", nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials_expired.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaExpired {
		t.Fatalf("status = %v", q.Status)
	}
	if q.Plan != "Pro" {
		t.Fatalf("plan = %q", q.Plan)
	}
	if *hits != 0 {
		t.Fatalf("expired token must not hit the network, got %d", *hits)
	}
}

func TestFetchClaude401And429(t *testing.T) {
	for _, tc := range []struct {
		code  int
		want  QuotaStatus
		after time.Duration
	}{
		{401, QuotaExpired, 0},
		{403, QuotaExpired, 0},
		{429, QuotaRateLimited, 90 * time.Second},
	} {
		srv, _ := claudeServer(t, tc.code, `{"error":"x"}`,
			map[string]string{"Retry-After": "90"})
		env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))
		q := FetchClaude(context.Background(), env)
		if q.Status != tc.want {
			t.Fatalf("HTTP %d → status %v, want %v", tc.code, q.Status, tc.want)
		}
		if q.RetryAfter != tc.after {
			t.Fatalf("HTTP %d → RetryAfter %v, want %v", tc.code, q.RetryAfter, tc.after)
		}
	}
}

func TestFetchClaude429NoHeader(t *testing.T) {
	srv, _ := claudeServer(t, 429, `{"error":"x"}`, nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaRateLimited {
		t.Fatalf("status = %v", q.Status)
	}
	if q.RetryAfter != 5*time.Minute {
		t.Fatalf("RetryAfter fallback = %v", q.RetryAfter)
	}
}

func TestFetchClaude500(t *testing.T) {
	srv, _ := claudeServer(t, 500, `{"error":"boom"}`, nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaFailed {
		t.Fatalf("status = %v", q.Status)
	}
	if q.Err == "" {
		t.Fatal("Err should carry a short description")
	}
}

func TestFetchClaudeMissingFields(t *testing.T) {
	srv, _ := claudeServer(t, 200, fixture(t, "claude_usage_missing_fields.json"), nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaOK {
		t.Fatalf("status = %v", q.Status)
	}
	if q.FiveHour == nil || q.FiveHour.UsedPct != 66 {
		t.Fatalf("five-hour = %+v", q.FiveHour)
	}
	if !q.FiveHour.ResetsAt.IsZero() {
		t.Fatalf("missing resets_at should leave zero time, got %v", q.FiveHour.ResetsAt)
	}
	if q.Weekly != nil {
		t.Fatalf("weekly should be nil, got %+v", q.Weekly)
	}
}

func TestFetchClaudeMissingUtilization(t *testing.T) {
	// A window object with no utilization at all is unknown data, not a
	// "100% left" healthy one.
	srv, _ := claudeServer(t, 200, `{"five_hour":{"resets_at":"2027-01-10T17:00:00Z"}}`, nil)
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaOK {
		t.Fatalf("status = %v", q.Status)
	}
	if q.FiveHour != nil {
		t.Fatalf("missing utilization must not publish a window, got %+v", q.FiveHour)
	}
}

func TestFetchClaudeRejectsRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirected request reached the target host with credentials in flight")
	}))
	defer target.Close()
	srv, _ := claudeServer(t, http.StatusTemporaryRedirect, "",
		map[string]string{"Location": target.URL + "/api/oauth/usage"})
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	q := FetchClaude(context.Background(), env)
	if q.Status != QuotaFailed {
		t.Fatalf("status = %v, want QuotaFailed (redirect refused)", q.Status)
	}
}

func TestFetchClaudeCancelledContext(t *testing.T) {
	// A cancelled context short-circuits before the Keychain is touched.
	touched := false
	srv, _ := claudeServer(t, 200, `{}`, nil)
	env := claudeEnv(srv, t.TempDir(),
		func(context.Context, string) ([]byte, error) { touched = true; return keychainMissing()(context.Background(), "") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := FetchClaude(ctx, env)
	if q.Status != QuotaFailed {
		t.Fatalf("status = %v, want QuotaFailed", q.Status)
	}
	if touched {
		t.Fatal("credentials were read despite a cancelled context")
	}
}

func TestFetchClaudeCancelledKeychainSkipsFallback(t *testing.T) {
	// A Keychain read that cancels the context must end the fetch: the
	// ~/.claude/.credentials.json fallback is a credential read too, and
	// the caller already asked to stop. With no fallback file present the
	// buggy path would end at "not logged in" instead of the cancel.
	srv, _ := claudeServer(t, 200, `{}`, nil)
	env := claudeEnv(srv, t.TempDir(), nil) // no credentials file in Home
	ctx, cancel := context.WithCancel(context.Background())
	env.Keychain = func(c context.Context, service string) ([]byte, error) {
		cancel()
		return nil, errNotLoggedIn // as if /usr/bin/security died on cancel
	}
	q := FetchClaude(ctx, env)
	if q.Status != QuotaFailed {
		t.Fatalf("status = %v, want QuotaFailed (cancel honoured before fallback)", q.Status)
	}
}

func TestFetchClaudeContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, "{}")
	}))
	defer srv.Close()
	env := claudeEnv(srv, t.TempDir(), keychainFixture(t, "claude_credentials.json"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	q := FetchClaude(ctx, env)
	if q.Status != QuotaFailed {
		t.Fatalf("status = %v", q.Status)
	}
}
