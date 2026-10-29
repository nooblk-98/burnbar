package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexExecutableDetection(t *testing.T) {
	t.Setenv("BURNBAR_CODEX_PATH", "")
	root := t.TempDir()
	t.Setenv("PATH", root)
	t.Setenv("LOCALAPPDATA", root)
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	desktop := filepath.Join(root, "Programs", "OpenAI", "Codex", "bin", "codex.exe")
	write(desktop)
	if got, err := codexExecutable(); err != nil || got != desktop {
		t.Fatalf("desktop CLI: %q, %v", got, err)
	}
	t.Setenv("BURNBAR_CODEX_PATH", filepath.Join(root, "custom", "codex.exe"))
	if got, err := codexExecutable(); err != nil || got != os.Getenv("BURNBAR_CODEX_PATH") {
		t.Fatalf("override: %q, %v", got, err)
	}
	t.Setenv("BURNBAR_CODEX_PATH", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "missing"))
	write(filepath.Join(root, "codex.cmd"))
	npm := filepath.Join(root, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "codex", "codex.exe")
	write(npm)
	if got, err := codexExecutable(); err != nil || got != npm {
		t.Fatalf("npm CLI: %q, %v", got, err)
	}
}

func TestParseCodexBuckets(t *testing.T) {
	u := ParseCodex([]byte(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":99}},"rateLimitsByLimitId":{
	"codex":{"limitId":"codex","primary":{"usedPercent":37,"windowDurationMins":300,"resetsAt":1793260800},"secondary":{"usedPercent":12,"windowDurationMins":10080,"resetsAt":1793865600}},
	"review":{"limitName":"Code review","primary":{"usedPercent":7,"windowDurationMins":60,"resetsAt":null}}
	}}`))
	if u.Err != "" || u.Provider != Codex || u.FiveHour == nil || u.SevenDay == nil {
		t.Fatalf("missing Codex limits: %+v", u)
	}
	if u.FiveHour.Percent != 37 || u.FiveHour.Length != 5*time.Hour || u.FiveHour.ResetsAt.Unix() != 1793260800 {
		t.Fatalf("primary: %+v", u.FiveHour)
	}
	if u.SevenDay.Percent != 12 || u.SevenDay.Length != 7*24*time.Hour || u.SevenDay.Label != "Weekly (7d)" {
		t.Fatalf("secondary: %+v", u.SevenDay)
	}
	if len(u.Extra) != 1 || u.Extra[0].Key != "review/primary" || !u.Extra[0].ResetsAt.IsZero() {
		t.Fatalf("extra buckets: %+v", u.Extra)
	}
}

func TestParseCodexLegacyAndNonstandardWindows(t *testing.T) {
	u := ParseCodex([]byte(`{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":15},"secondary":{"usedPercent":100,"windowDurationMins":60}}}`))
	if u.Err != "" || u.FiveHour == nil || u.FiveHour.Percent != 0 || u.FiveHour.Length != 15*time.Minute {
		t.Fatalf("zero usage and dynamic duration: %+v", u)
	}
	if u.SevenDay == nil || u.SevenDay.Label != "Secondary (1h)" {
		t.Fatalf("nonweekly label: %+v", u.SevenDay)
	}
	if _, ok := u.FiveHour.Elapsed(); ok {
		t.Fatal("missing reset must not create a pace marker")
	}
}

func TestParseCodexUnavailable(t *testing.T) {
	for _, body := range []string{`not json`, `null`, `{}`, `{"rateLimits":null}`, `{"rateLimits":{"primary":{"usedPercent":null}}}`, `{"rateLimits":{"primary":{"usedPercent":-1}}}`, `{"rateLimits":{"primary":{"usedPercent":101}}}`} {
		t.Run(body, func(t *testing.T) {
			u := ParseCodex([]byte(body))
			if u.Err == "" || len(u.All()) != 0 {
				t.Fatalf("must show unavailable, not zero: %+v", u)
			}
		})
	}
}

func TestCodexRPCHandshakeAndNotifications(t *testing.T) {
	input := strings.NewReader(`{"method":"account/updated","params":{}}
{"id":1,"result":{"userAgent":"test"}}
{"id":"server-request","method":"account/chatgptAuthTokens/refresh","params":{}}
{"id":2,"result":{"account":{"type":"chatgpt"}}}
{"id":3,"result":{"rateLimits":{"primary":{"usedPercent":42}}}}
`)
	var output bytes.Buffer
	rpc := newCodexRPC(input, &output)
	for i, method := range []string{"initialize", "account/read", "account/rateLimits/read"} {
		body, err := rpc.call(i+1, method, map[string]any{})
		if err != nil || !json.Valid(body) {
			t.Fatalf("%s: %s, %v", method, body, err)
		}
	}
	var requests []map[string]any
	dec := json.NewDecoder(&output)
	for dec.More() {
		var request map[string]any
		if err := dec.Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	if len(requests) != 4 || requests[2]["id"] != "server-request" || requests[2]["error"] == nil {
		t.Fatalf("server request must be rejected: %+v", requests)
	}
}

func TestCodexRPCFailures(t *testing.T) {
	for _, tc := range []struct {
		body        string
		rateLimited bool
	}{
		{`{"id":1,"error":{"code":429,"message":"private details"}}`, true},
		{`{"id":1,"error":{"code":-32601,"message":"private details"}}`, false},
		{`{"id":1}`, false}, {`bad json`, false}, {``, false},
	} {
		_, err := newCodexRPC(strings.NewReader(tc.body), &bytes.Buffer{}).call(1, "initialize", nil)
		if err == nil || strings.Contains(err.Error(), "private details") || errors.Is(err, errRateLimited) != tc.rateLimited {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
