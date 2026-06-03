package main

import (
	"fmt"
	"strconv"
	"time"
)

// ParseFor parses the value of --for. It accepts any Go duration that
// time.ParseDuration understands (1h30m, 90m, 45s) or a bare integer, which is
// interpreted as a number of seconds (e.g. "5400" == 1h30m). The result must be
// positive.
func ParseFor(s string) (time.Duration, error) {
	// A bare integer is treated as seconds. Try that first so "90" doesn't get
	// rejected by ParseDuration (which requires a unit).
	if n, err := strconv.Atoi(s); err == nil {
		d := time.Duration(n) * time.Second
		if d <= 0 {
			return 0, fmt.Errorf("can't parse --for %q\n       use a duration like 90m, 1h30m, 45s (or seconds: 5400)", s)
		}
		return d, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("can't parse --for %q\n       use a duration like 90m, 1h30m, 45s (or seconds: 5400)", s)
	}
	return d, nil
}

// ParseUntil parses the value of --until as a 24-hour clock time (HH:MM) today.
// now is passed in so the function stays pure and testable. If the requested
// time has already passed today, it returns an error rather than rolling over
// to tomorrow.
func ParseUntil(s string, now time.Time) (time.Time, error) {
	t, err := time.ParseInLocation("15:04", s, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("can't parse --until %q\n       use a 24-hour clock time like 17:00 or 09:30", s)
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !target.After(now) {
		return time.Time{}, fmt.Errorf("--until %s is in the past (it's %s now); use --for for a relative duration",
			s, now.Format("15:04"))
	}
	return target, nil
}

// shortDur formats a duration the way the start/status lines want it: hours and
// minutes for anything a minute or longer (e.g. "2h0m", "1h28m", "45m"), and
// seconds for sub-minute spans ("45s"). It rounds to whole minutes for the
// minute-and-up case so the line stays tidy.
func shortDur(d time.Duration) string {
	if d < time.Minute {
		secs := int(d.Round(time.Second) / time.Second)
		if secs < 1 {
			secs = 1
		}
		return strconv.Itoa(secs) + "s"
	}
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}
