// Package usage reads plan limits for Claude Code and Codex.
package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Window is one usage limit, e.g. the 5-hour session or the 7-day week.
type Window struct {
	Key      string
	Label    string
	Percent  float64
	ResetsAt time.Time
	Length   time.Duration
}

// Elapsed reports how far through its window the limit is (0..1), for the pace marker.
func (w *Window) Elapsed() (float64, bool) {
	if w == nil || w.ResetsAt.IsZero() || w.Length <= 0 {
		return 0, false
	}
	f := 1 - float64(time.Until(w.ResetsAt))/float64(w.Length)
	return max(0, min(1, f)), true
}

type Share struct {
	Name    string
	Percent float64
}

type Usage struct {
	Provider    Provider
	FiveHour    *Window
	SevenDay    *Window
	Extra       []*Window // any other limits the API reports (e.g. per-model weekly limits)
	Breakdown   []Share   // this week's usage split by product
	UpdatedAt   time.Time
	Err         string
	RateLimited bool
}

func (u Usage) Name() string { return u.Provider.Name() }

func (u Usage) All() []*Window {
	var all []*Window
	for _, w := range append([]*Window{u.FiveHour, u.SevenDay}, u.Extra...) {
		if w != nil {
			all = append(all, w)
		}
	}
	return all
}

const apiURL = "https://api.anthropic.com/api/oauth/usage"

// UserAgent is sent with API requests; main sets it to include the build version.
var UserAgent = "burnbar/dev"

var (
	httpClient     = &http.Client{Timeout: 20 * time.Second}
	errRateLimited = errors.New("usage API rate limited, backing off")
)

// ClaudeDir is Claude Code's configuration folder.
func ClaudeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func credentialsPath() string { return filepath.Join(ClaudeDir(), ".credentials.json") }

// readToken returns the OAuth access token Claude Code stores after `claude` login.
// The widget only reads it; Claude Code itself refreshes it when it runs.
func readToken() (string, error) {
	b, err := os.ReadFile(credentialsPath())
	if err != nil {
		return "", fmt.Errorf("no Claude Code login found (%s)", credentialsPath())
	}
	var c struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &c); err != nil || c.ClaudeAiOauth.AccessToken == "" {
		return "", errors.New("Claude Code credentials file has no OAuth token")
	}
	if exp := c.ClaudeAiOauth.ExpiresAt; exp > 0 && time.UnixMilli(exp).Before(time.Now()) {
		return "", errors.New("login token expired - run `claude` once to refresh it")
	}
	return c.ClaudeAiOauth.AccessToken, nil
}

// FetchRaw returns the usage API's JSON response.
func FetchRaw() ([]byte, error) {
	token, err := readToken()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("login rejected - run `claude` to sign in again")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, errRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("usage API returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// Fetch returns the current usage; failures are reported in Usage.Err.
func Fetch() Usage {
	body, err := FetchRaw()
	if err != nil {
		return Usage{Provider: Claude, Err: err.Error(), RateLimited: errors.Is(err, errRateLimited), UpdatedAt: time.Now()}
	}
	return Parse(body)
}

type apiWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func validPercent(v float64) bool { return v >= 0 && v <= 100 }

func parseTime(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, *s)
	return t
}

func limitLabel(kind string) string {
	switch kind {
	case "session":
		return "Session (5h)"
	case "weekly_all":
		return "Weekly (all models)"
	}
	words := strings.Fields(strings.ReplaceAll(kind, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func windowLength(kind, group string) time.Duration {
	switch {
	case group == "session" || kind == "session":
		return 5 * time.Hour
	case group == "weekly" || strings.HasPrefix(kind, "weekly"):
		return 7 * 24 * time.Hour
	}
	return 0
}

// Parse decodes a usage API response.
func Parse(body []byte) Usage {
	var r struct {
		FiveHour *apiWindow `json:"five_hour"`
		SevenDay *apiWindow `json:"seven_day"`
		Limits   []struct {
			Kind     string   `json:"kind"`
			Group    string   `json:"group"`
			Percent  *float64 `json:"percent"`
			ResetsAt *string  `json:"resets_at"`
		} `json:"limits"`
		Breakdown *struct {
			Rows []struct {
				Name    string  `json:"display_name"`
				Percent float64 `json:"percent"`
			} `json:"rows"`
		} `json:"seven_day_breakdown"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Usage{Provider: Claude, Err: "unexpected response from usage API", UpdatedAt: time.Now()}
	}
	u := Usage{Provider: Claude, UpdatedAt: time.Now()}
	invalid := func() Usage {
		return Usage{Provider: Claude, Err: "invalid usage percentage from Claude", UpdatedAt: u.UpdatedAt}
	}
	for _, w := range []*apiWindow{r.FiveHour, r.SevenDay} {
		if w != nil && w.Utilization != nil && !validPercent(*w.Utilization) {
			return invalid()
		}
	}
	for _, l := range r.Limits {
		if l.Percent == nil {
			continue
		}
		if !validPercent(*l.Percent) {
			return invalid()
		}
		if l.Kind == "" {
			continue
		}
		w := &Window{Key: l.Kind, Label: limitLabel(l.Kind), Percent: *l.Percent,
			ResetsAt: parseTime(l.ResetsAt), Length: windowLength(l.Kind, l.Group)}
		switch l.Kind {
		case "session":
			u.FiveHour = w
		case "weekly_all":
			u.SevenDay = w
		default:
			u.Extra = append(u.Extra, w)
		}
	}
	// Older responses only carry the five_hour / seven_day objects.
	if u.FiveHour == nil && r.FiveHour != nil && r.FiveHour.Utilization != nil {
		u.FiveHour = &Window{Key: "session", Label: limitLabel("session"), Percent: *r.FiveHour.Utilization,
			ResetsAt: parseTime(r.FiveHour.ResetsAt), Length: 5 * time.Hour}
	}
	if u.SevenDay == nil && r.SevenDay != nil && r.SevenDay.Utilization != nil {
		u.SevenDay = &Window{Key: "weekly_all", Label: limitLabel("weekly_all"), Percent: *r.SevenDay.Utilization,
			ResetsAt: parseTime(r.SevenDay.ResetsAt), Length: 7 * 24 * time.Hour}
	}
	if r.Breakdown != nil {
		for _, row := range r.Breakdown.Rows {
			if row.Percent > 0 && validPercent(row.Percent) {
				u.Breakdown = append(u.Breakdown, Share{row.Name, row.Percent})
			}
		}
	}
	if len(u.All()) == 0 {
		u.Err = "Claude returned no plan usage limits"
	}
	return u
}

// Demo returns sample numbers for -demo mode.
func Demo() Usage {
	now := time.Now()
	return Usage{
		Provider: Claude,
		FiveHour: &Window{Key: "session", Label: limitLabel("session"), Percent: 62,
			ResetsAt: now.Add(1*time.Hour + 13*time.Minute), Length: 5 * time.Hour},
		SevenDay: &Window{Key: "weekly_all", Label: limitLabel("weekly_all"), Percent: 17,
			ResetsAt: now.Add(4*24*time.Hour + 6*time.Hour), Length: 7 * 24 * time.Hour},
		Extra: []*Window{{Key: "weekly_opus", Label: limitLabel("weekly_opus"), Percent: 35,
			ResetsAt: now.Add(4*24*time.Hour + 6*time.Hour), Length: 7 * 24 * time.Hour}},
		Breakdown: []Share{{"Claude Code", 91}, {"Chats", 7}, {"Cowork", 2}},
		UpdatedAt: now,
	}
}
