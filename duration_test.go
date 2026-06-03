package main

import (
	"testing"
	"time"
)

func TestParseFor(t *testing.T) {
	good := []struct {
		in   string
		want time.Duration
	}{
		{"1h30m", 90 * time.Minute},
		{"90m", 90 * time.Minute},
		{"45s", 45 * time.Second},
		{"5400", 90 * time.Minute}, // bare int = seconds
		{"1", time.Second},
		{"2h", 2 * time.Hour},
	}
	for _, c := range good {
		got, err := ParseFor(c.in)
		if err != nil {
			t.Errorf("ParseFor(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseFor(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	bad := []string{"2hours", "", "abc", "0", "-5", "-1h", "1.5x", "90 m"}
	for _, in := range bad {
		if _, err := ParseFor(in); err == nil {
			t.Errorf("ParseFor(%q) = nil error, want error", in)
		}
	}
}

func TestParseUntil(t *testing.T) {
	now := time.Date(2026, 6, 3, 14, 30, 0, 0, time.Local)

	// Future time today -> exact target.
	target, err := ParseUntil("17:00", now)
	if err != nil {
		t.Fatalf("ParseUntil(17:00) unexpected error: %v", err)
	}
	want := time.Date(2026, 6, 3, 17, 0, 0, 0, time.Local)
	if !target.Equal(want) {
		t.Errorf("ParseUntil(17:00) = %v, want %v", target, want)
	}

	// Past time today -> error, not a rollover.
	if _, err := ParseUntil("09:00", now); err == nil {
		t.Error("ParseUntil(09:00) at 14:30 = nil error, want past error")
	}

	// Equal to now -> not after now -> error.
	if _, err := ParseUntil("14:30", now); err == nil {
		t.Error("ParseUntil(14:30) at 14:30 = nil error, want past error")
	}

	// Malformed inputs.
	for _, in := range []string{"25:00", "9am", "abc", "", "17", "17:60"} {
		if _, err := ParseUntil(in, now); err == nil {
			t.Errorf("ParseUntil(%q) = nil error, want error", in)
		}
	}
}

func TestShortDur(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{2 * time.Hour, "2h0m"},
		{90 * time.Minute, "1h30m"},
		{45 * time.Minute, "45m"},
		{45 * time.Second, "45s"},
		{500 * time.Millisecond, "1s"},
		{88 * time.Minute, "1h28m"},
	}
	for _, c := range cases {
		if got := shortDur(c.in); got != c.want {
			t.Errorf("shortDur(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
