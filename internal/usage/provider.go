package usage

import "time"

type Provider string

const (
	Claude Provider = "claude"
	Codex  Provider = "codex"
)

func (p Provider) Name() string {
	if p == Codex {
		return "Codex"
	}
	return "Claude"
}

func ValidMode(mode string) bool { return mode == "claude" || mode == "codex" || mode == "both" }

func Providers(mode string) []Provider {
	switch mode {
	case "codex":
		return []Provider{Codex}
	case "both":
		return []Provider{Claude, Codex}
	default:
		return []Provider{Claude}
	}
}

func FetchProvider(p Provider) Usage {
	if p == Codex {
		return FetchCodex()
	}
	return Fetch()
}

func DemoProvider(p Provider) Usage {
	u := Demo()
	u.Provider = p
	if p == Codex {
		u.FiveHour = &Window{Key: "primary", Label: "Session (5h)", Percent: 38,
			ResetsAt: time.Now().Add(2 * time.Hour), Length: 5 * time.Hour}
		u.SevenDay = &Window{Key: "secondary", Label: "Weekly (7d)", Percent: 24,
			ResetsAt: time.Now().Add(3 * 24 * time.Hour), Length: 7 * 24 * time.Hour}
		u.Extra, u.Breakdown = nil, nil
	}
	return u
}
