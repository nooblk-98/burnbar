package usage

import "testing"

func TestParse(t *testing.T) {
	body := []byte(`{"five_hour":{"utilization":49.0,"resets_at":"2026-10-29T07:40:00.369326+00:00"},
	"limits":[{"kind":"session","group":"session","percent":49,"resets_at":"2026-10-29T07:40:00.369326+00:00"},
	{"kind":"weekly_all","group":"weekly","percent":13,"resets_at":"2026-11-03T23:00:00+00:00"},
	{"kind":"weekly_opus","group":"weekly","percent":40,"resets_at":"2026-11-03T23:00:00+00:00"}],
	"seven_day_breakdown":{"rows":[{"display_name":"Claude Code","percent":97},{"display_name":"Other","percent":0}]}}`)
	u := Parse(body)
	if u.FiveHour == nil || u.FiveHour.Percent != 49 || u.SevenDay == nil || u.SevenDay.Percent != 13 {
		t.Fatalf("main limits not parsed: %+v", u)
	}
	if len(u.Extra) != 1 || u.Extra[0].Label != "Weekly Opus" {
		t.Errorf("extra limits: %+v", u.Extra)
	}
	if len(u.Breakdown) != 1 || u.Breakdown[0].Name != "Claude Code" {
		t.Errorf("breakdown: %+v", u.Breakdown)
	}
}

func TestParseLegacy(t *testing.T) {
	u := Parse([]byte(`{"five_hour":{"utilization":5,"resets_at":null},"seven_day":{"utilization":3,"resets_at":null}}`))
	if u.FiveHour == nil || u.FiveHour.Percent != 5 || u.SevenDay == nil || u.SevenDay.Percent != 3 {
		t.Fatalf("legacy fields not parsed: %+v", u)
	}
}
