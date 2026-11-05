package alerts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func DefaultThresholds() []int { return []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100} }

// ParseThresholds accepts explicit percentages; an empty list disables that quota.
func ParseThresholds(text string) ([]int, error) {
	values := []int{}
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		n, err := strconv.Atoi(strings.TrimSuffix(part, "%"))
		if err != nil || n < 1 || n > 100 {
			return nil, fmt.Errorf("thresholds must be whole percentages from 1 to 100")
		}
		if !seen[n] {
			values = append(values, n)
			seen[n] = true
		}
	}
	sort.Ints(values)
	return values, nil
}

func ThresholdText(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

func NormalizeThresholds(values []int) []int {
	clean := []int{}
	for _, v := range values {
		if v >= 1 && v <= 100 {
			clean = append(clean, v)
		}
	}
	result, _ := ParseThresholds(ThresholdText(clean))
	return result
}

func ParseClock(value string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("use 24-hour HH:MM, for example 22:00")
	}
	return t.Hour()*60 + t.Minute(), nil
}
func Clock(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

// Quiet uses local wall-clock time, including overnight ranges. Equal endpoints
// mean a full quiet day; the enable switch distinguishes this from disabled.
func Quiet(now time.Time, snoozeUntil int64, enabled bool, start, end int) bool {
	if snoozeUntil > now.Unix() {
		return true
	}
	if !enabled {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if start == end {
		return true
	}
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

func Tomorrow(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, now.Location())
}
