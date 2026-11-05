// Package updates checks only public stable release metadata; it never installs.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const Endpoint = "https://api.github.com/repos/nooblk-98/burnbar/releases/latest"
const Command = "npx --yes burnbar@latest update"
const Releases = "https://github.com/nooblk-98/burnbar/releases"

type Release struct {
	Version string
	URL     string
}

func stable(s string) ([3]int, bool) {
	var result [3]int
	s = strings.TrimPrefix(s, "v")
	if strings.ContainsAny(s, "-+") {
		return result, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, p := range parts {
		if p == "" || len(p) > 1 && p[0] == '0' {
			return result, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return result, false
			}
		}
		n, e := strconv.Atoi(p)
		if e != nil {
			return result, false
		}
		result[i] = n
	}
	return result, true
}
func Newer(current, candidate string) bool {
	a, ok := stable(current)
	if !ok {
		return false
	}
	b, ok := stable(candidate)
	if !ok {
		return false
	}
	for i := range a {
		if b[i] != a[i] {
			return b[i] > a[i]
		}
	}
	return false
}
func Check(ctx context.Context, client *http.Client) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BurnBar update check")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub returned HTTP %d; try again later", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024+1))
	if err != nil || len(body) > 256*1024 {
		return Release{}, fmt.Errorf("could not read release metadata")
	}
	var data struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(body, &data) != nil {
		return Release{}, fmt.Errorf("invalid release metadata")
	}
	if _, ok := stable(data.Tag); !ok || data.Draft || data.Prerelease {
		return Release{}, fmt.Errorf("no stable release available")
	}
	u, e := url.Parse(data.URL)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/nooblk-98/burnbar/releases/tag/"+data.Tag {
		return Release{}, fmt.Errorf("invalid release link")
	}
	return Release{Version: strings.TrimPrefix(data.Tag, "v"), URL: data.URL}, nil
}
