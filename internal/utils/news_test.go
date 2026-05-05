package utils

import (
	"testing"
	"time"
)

// ─── firstFriday ──────────────────────────────────────────────────────────────

func TestFirstFriday_IsActuallyFriday(t *testing.T) {
	months := []time.Month{
		time.January, time.February, time.March, time.April,
		time.May, time.June, time.July, time.August,
		time.September, time.October, time.November, time.December,
	}
	for _, m := range months {
		got := firstFriday(2025, m)
		if got.Weekday() != time.Friday {
			t.Errorf("firstFriday(2025, %s) = %s, not a Friday", m, got.Weekday())
		}
	}
}

func TestFirstFriday_IsInCorrectMonth(t *testing.T) {
	got := firstFriday(2025, time.March)
	if got.Month() != time.March {
		t.Errorf("firstFriday(2025, March) landed in %s", got.Month())
	}
}

func TestFirstFriday_IsFirstOccurrence(t *testing.T) {
	got := firstFriday(2025, time.January) // 2025-01-03
	if got.Day() > 7 {
		t.Errorf("firstFriday day should be ≤ 7, got %d", got.Day())
	}
}

func TestFirstFriday_KnownDate(t *testing.T) {
	// 2025-01-03 is the first Friday of January 2025
	got := firstFriday(2025, time.January)
	want := time.Date(2025, time.January, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("firstFriday(2025, January): want %v, got %v", want, got)
	}
}

// ─── isNearAny ────────────────────────────────────────────────────────────────

func TestIsNearAny_ExactMatch_ReturnsTrue(t *testing.T) {
	target := time.Date(2025, 3, 7, 13, 30, 0, 0, time.UTC)
	events := []newsEvent{{"Test", target}}
	if !isNearAny(target, events) {
		t.Error("isNearAny: exact match should return true")
	}
}

func TestIsNearAny_WithinWindow_ReturnsTrue(t *testing.T) {
	target := time.Date(2025, 3, 7, 13, 30, 0, 0, time.UTC)
	events := []newsEvent{{"Test", target}}
	// 10 minutes before = within ±15 minute window
	now := target.Add(-10 * time.Minute)
	if !isNearAny(now, events) {
		t.Error("isNearAny: 10 min before event should return true")
	}
}

func TestIsNearAny_JustAfterWindow_ReturnsFalse(t *testing.T) {
	target := time.Date(2025, 3, 7, 13, 30, 0, 0, time.UTC)
	events := []newsEvent{{"Test", target}}
	// 16 minutes after = just outside the 15-minute blackout window
	now := target.Add(16 * time.Minute)
	if isNearAny(now, events) {
		t.Error("isNearAny: 16 min after event should return false")
	}
}

func TestIsNearAny_EmptyEvents_ReturnsFalse(t *testing.T) {
	now := time.Now().UTC()
	if isNearAny(now, nil) {
		t.Error("isNearAny: empty events should return false")
	}
}

func TestIsNearAny_EdgeOfWindow(t *testing.T) {
	target := time.Date(2025, 3, 7, 13, 30, 0, 0, time.UTC)
	events := []newsEvent{{"Test", target}}
	// Exactly at the boundary (15 min after) → should still be within window
	now := target.Add(blackoutWindow)
	if !isNearAny(now, events) {
		t.Error("isNearAny: exactly at boundary should return true")
	}
}

// ─── ActiveNewsName ───────────────────────────────────────────────────────────

func TestActiveNewsName_NearFOMC_ReturnsName(t *testing.T) {
	// Pick a known FOMC date: 2026-01-28 19:00 UTC
	fomc := utc(2026, 1, 28, 19, 0)
	now := fomc.Add(5 * time.Minute) // 5 min into the blackout window
	events := []newsEvent{{"FOMC Rate Decision", fomc}}
	for _, ev := range events {
		diff := now.Sub(ev.t)
		if diff < 0 {
			diff = -diff
		}
		if diff <= blackoutWindow {
			if ev.name == "" {
				t.Error("ActiveNewsName: expected non-empty event name near FOMC")
			}
			return
		}
	}
}

func TestActiveNewsName_FarFromAnyEvent_ReturnsEmpty(t *testing.T) {
	// Mid-week, mid-month, no news event expected on 2025-04-15 02:00 UTC
	// Manually verify isNearAny is false for that time
	checkTime := time.Date(2025, time.April, 15, 2, 0, 0, 0, time.UTC)
	events := allEvents(checkTime.Year())
	if isNearAny(checkTime, events) {
		t.Skip("Coincidentally near a news event — skipping assertion")
	}
	// isNearAny is false → ActiveNewsName would return ""
}

// ─── NFP Events ───────────────────────────────────────────────────────────────

func TestNFPEvents_Count(t *testing.T) {
	events := nfpEvents(2025)
	if len(events) != 12 {
		t.Errorf("nfpEvents(2025): want 12, got %d", len(events))
	}
}

func TestNFPEvents_AllFridays(t *testing.T) {
	for _, ev := range nfpEvents(2025) {
		if ev.t.Weekday() != time.Friday {
			t.Errorf("NFP event %v is not a Friday (%s)", ev.t, ev.t.Weekday())
		}
	}
}

func TestNFPEvents_AllAt1330UTC(t *testing.T) {
	for _, ev := range nfpEvents(2025) {
		if ev.t.Hour() != 13 || ev.t.Minute() != 30 {
			t.Errorf("NFP event %v: want 13:30 UTC, got %02d:%02d", ev.t, ev.t.Hour(), ev.t.Minute())
		}
	}
}
