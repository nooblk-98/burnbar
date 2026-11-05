package usage

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type rateLimitError struct{ until time.Time }

func (e rateLimitError) Error() string { return errRateLimited.Error() }
func (e rateLimitError) Unwrap() error { return errRateLimited }

func retryAfter(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at
	}
	return time.Time{}
}

// PreserveLastGood never relabels an unsuccessful attempt as fresh usage.
func PreserveLastGood(next, prev Usage) Usage {
	if next.Err == "" {
		return next
	}
	next.FiveHour, next.SevenDay, next.Extra, next.Breakdown = prev.FiveHour, prev.SevenDay, prev.Extra, prev.Breakdown
	next.UpdatedAt = time.Time{}
	if len(prev.All()) > 0 {
		next.UpdatedAt = prev.UpdatedAt
	}
	return next
}
