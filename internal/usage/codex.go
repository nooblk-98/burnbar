package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexDir honors the CLI's alternate configuration directory.
func CodexDir() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// codexExecutable finds the native CLI, including npm's Windows installation.
// No command shell is used, and an override must point directly at an executable.
func codexExecutable() (string, error) {
	if path := os.Getenv("BURNBAR_CODEX_PATH"); path != "" {
		return path, nil
	}
	if path, err := exec.LookPath("codex.exe"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("codex"); err == nil && !strings.EqualFold(filepath.Ext(path), ".cmd") && !strings.EqualFold(filepath.Ext(path), ".bat") {
		return path, nil
	}
	// The Windows desktop app installs a CLI even when it is absent from PATH.
	local := os.Getenv("LOCALAPPDATA")
	if local != "" {
		path := filepath.Join(local, "Programs", "OpenAI", "Codex", "bin", "codex.exe")
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, nil
		}
		matches, _ := filepath.Glob(filepath.Join(local, "OpenAI", "Codex", "bin", "*", "codex.exe"))
		var newest string
		var modified time.Time
		for _, path := range matches {
			if st, err := os.Stat(path); err == nil && !st.IsDir() && st.ModTime().After(modified) {
				newest, modified = path, st.ModTime()
			}
		}
		if newest != "" {
			return newest, nil
		}
	}
	if shim, err := exec.LookPath("codex.cmd"); err == nil {
		root := filepath.Join(filepath.Dir(shim), "node_modules", "@openai", "codex")
		for _, pattern := range []string{
			filepath.Join(root, "node_modules", "@openai", "codex-win32-*", "vendor", "*", "codex", "codex.exe"),
			filepath.Join(root, "vendor", "*", "codex", "codex.exe"),
		} {
			matches, _ := filepath.Glob(pattern)
			for _, path := range matches {
				if st, err := os.Stat(path); err == nil && !st.IsDir() {
					return path, nil
				}
			}
		}
	}
	return "", errors.New("install Codex CLI, then run `codex login`")
}

// FetchCodexRaw uses only the documented account RPCs. It never starts a model
// turn, reads tokens itself, or opens a login flow.
func FetchCodexRaw() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	path, err := codexExecutable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "app-server")
	cmd.WaitDelay = time.Second
	hideConsole(cmd)
	// Account requests do not need the caller's project configuration.
	cmd.Dir, _ = os.UserHomeDir()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("could not open Codex usage connection")
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		out.Close()
		return nil, errors.New("could not open Codex usage connection")
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, errors.New("could not start Codex CLI - check its installation")
	}
	defer func() {
		in.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()
	rpc := newCodexRPC(out, in)
	_, err = rpc.call(1, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "burnbar", "title": "BurnBar", "version": strings.TrimPrefix(UserAgent, "burnbar/")},
	})
	if err != nil {
		return nil, codexConnectionError(ctx, err)
	}
	if err := rpc.enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, errors.New("Codex usage connection closed")
	}
	account, err := rpc.call(2, "account/read", map[string]any{"refreshToken": false})
	if err != nil {
		return nil, codexConnectionError(ctx, err)
	}
	var auth struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if json.Unmarshal(account, &auth) != nil || auth.Account == nil || auth.Account.Type == "apiKey" {
		return nil, errors.New("sign in to Codex with ChatGPT: `codex login`")
	}
	body, err := rpc.call(3, "account/rateLimits/read", map[string]any{})
	if err != nil {
		return nil, codexConnectionError(ctx, err)
	}
	return body, nil
}

func codexConnectionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("Codex usage request timed out")
	}
	return err
}

type codexRPC struct {
	scan *bufio.Scanner
	enc  *json.Encoder
}

func newCodexRPC(r io.Reader, w io.Writer) *codexRPC {
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 2<<20)
	return &codexRPC{scan: scan, enc: json.NewEncoder(w)}
}

func (r *codexRPC) call(id int, method string, params any) (json.RawMessage, error) {
	if err := r.enc.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, errors.New("Codex usage connection closed")
	}
	for r.scan.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(r.scan.Bytes(), &msg) != nil {
			return nil, errors.New("unexpected response from Codex CLI")
		}
		if msg.Method != "" {
			// Reject server requests rather than letting a local account probe
			// trigger tools, authentication, or other interactive actions.
			if len(msg.ID) > 0 && string(msg.ID) != "null" {
				if err := r.enc.Encode(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "unsupported method"}}); err != nil {
					return nil, errors.New("Codex usage connection closed")
				}
			}
			continue
		}
		var responseID int
		if json.Unmarshal(msg.ID, &responseID) != nil || responseID != id {
			continue
		}
		if msg.Error != nil {
			if msg.Error.Code == 429 {
				return nil, errRateLimited
			}
			// Do not surface arbitrary server text that could contain account data.
			return nil, errors.New("Codex usage unavailable - sign in or update Codex CLI")
		}
		if len(msg.Result) == 0 {
			return nil, errors.New("unexpected response from Codex CLI")
		}
		return msg.Result, nil
	}
	return nil, errors.New("Codex usage connection closed")
}

func FetchCodex() Usage {
	body, err := FetchCodexRaw()
	if err != nil {
		return Usage{Provider: Codex, UpdatedAt: time.Now(), Err: err.Error(), RateLimited: errors.Is(err, errRateLimited)}
	}
	return ParseCodex(body)
}

type codexWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int64   `json:"windowDurationMins"`
	ResetsAt           *int64   `json:"resetsAt"`
}

type codexLimits struct {
	LimitID   string       `json:"limitId"`
	LimitName string       `json:"limitName"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

// ParseCodex supports both the legacy single bucket and the current multi-bucket
// account response. Durations come from Codex instead of assuming 5h/7d quotas.
func ParseCodex(body []byte) Usage {
	u := Usage{Provider: Codex, UpdatedAt: time.Now()}
	var result struct {
		RateLimits          *codexLimits           `json:"rateLimits"`
		RateLimitsByLimitID map[string]codexLimits `json:"rateLimitsByLimitId"`
	}
	if json.Unmarshal(body, &result) != nil {
		u.Err = "unexpected response from Codex usage"
		return u
	}
	main, found := result.RateLimitsByLimitID["codex"]
	if !found && result.RateLimits != nil {
		main, found = *result.RateLimits, true
	}
	toWindow := func(w *codexWindow, key, label string) *Window {
		if w == nil || w.UsedPercent == nil || !validPercent(*w.UsedPercent) {
			return nil
		}
		win := &Window{Key: key, Label: label, Percent: *w.UsedPercent}
		if w.WindowDurationMins != nil && *w.WindowDurationMins > 0 && *w.WindowDurationMins <= int64((365*24*time.Hour)/time.Minute) {
			win.Length = time.Duration(*w.WindowDurationMins) * time.Minute
			win.Label += " (" + WindowShortLabel(win, "") + ")"
		}
		if w.ResetsAt != nil && *w.ResetsAt > 0 {
			win.ResetsAt = time.Unix(*w.ResetsAt, 0)
		}
		return win
	}
	if found {
		u.FiveHour = toWindow(main.Primary, "primary", "Session")
		u.SevenDay = toWindow(main.Secondary, "secondary", "Weekly")
		// Nonstandard secondary windows must not be called weekly.
		if u.SevenDay != nil && u.SevenDay.Length != 7*24*time.Hour {
			u.SevenDay.Label = strings.Replace(u.SevenDay.Label, "Weekly", "Secondary", 1)
		}
	}
	keys := make([]string, 0, len(result.RateLimitsByLimitID))
	for key := range result.RateLimitsByLimitID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		bucket := result.RateLimitsByLimitID[key]
		if key == "codex" || (found && main.LimitID != "" && (key == main.LimitID || bucket.LimitID == main.LimitID)) {
			continue
		}
		name := bucket.LimitName
		if name == "" {
			name = key
		}
		for _, w := range []*Window{toWindow(bucket.Primary, key+"/primary", name), toWindow(bucket.Secondary, key+"/secondary", name+" secondary")} {
			if w != nil {
				u.Extra = append(u.Extra, w)
			}
		}
	}
	if len(u.All()) == 0 {
		u.Err = "Codex returned no plan usage limits"
	}
	return u
}

func WindowShortLabel(w *Window, fallback string) string {
	if w == nil || w.Length <= 0 {
		return fallback
	}
	if w.Length%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", w.Length/(24*time.Hour))
	}
	if w.Length%time.Hour == 0 {
		return fmt.Sprintf("%dh", w.Length/time.Hour)
	}
	return fmt.Sprintf("%dm", w.Length/time.Minute)
}
