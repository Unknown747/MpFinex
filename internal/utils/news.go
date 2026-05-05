// Package utils menyediakan fungsi utilitas umum yang digunakan seluruh Finex Bot.
package utils

import "time"

// ─── News Event Types ─────────────────────────────────────────────────────────

type newsEvent struct {
	name string
	t    time.Time // waktu rilis tepat dalam UTC
}

// blackoutWindow adalah jeda sebelum dan sesudah setiap rilis berita berdampak tinggi.
const blackoutWindow = 15 * time.Minute

// ─── FOMC Dates (8x/tahun) ────────────────────────────────────────────────────
// Waktu pengumuman: 19:00 UTC (pukul 14:00 ET).

var fomcDates2025 = []newsEvent{
	{"FOMC Rate Decision", utc(2025, 1, 29, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 3, 19, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 5, 7, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 6, 18, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 7, 30, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 9, 17, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 10, 29, 19, 0)},
	{"FOMC Rate Decision", utc(2025, 12, 10, 19, 0)},
}

var fomcDates2026 = []newsEvent{
	{"FOMC Rate Decision", utc(2026, 1, 28, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 3, 18, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 4, 29, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 6, 17, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 7, 29, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 9, 16, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 10, 28, 19, 0)},
	{"FOMC Rate Decision", utc(2026, 12, 9, 19, 0)},
}

// ─── CPI Dates (bulanan) ──────────────────────────────────────────────────────
// Waktu rilis: 13:30 UTC (pukul 08:30 ET).

var cpiDates2025 = []newsEvent{
	{"US CPI", utc(2025, 1, 15, 13, 30)},
	{"US CPI", utc(2025, 2, 12, 13, 30)},
	{"US CPI", utc(2025, 3, 12, 13, 30)},
	{"US CPI", utc(2025, 4, 10, 13, 30)},
	{"US CPI", utc(2025, 5, 13, 13, 30)},
	{"US CPI", utc(2025, 6, 11, 13, 30)},
	{"US CPI", utc(2025, 7, 11, 13, 30)},
	{"US CPI", utc(2025, 8, 12, 13, 30)},
	{"US CPI", utc(2025, 9, 10, 13, 30)},
	{"US CPI", utc(2025, 10, 9, 13, 30)},
	{"US CPI", utc(2025, 11, 13, 13, 30)},
	{"US CPI", utc(2025, 12, 10, 13, 30)},
}

var cpiDates2026 = []newsEvent{
	{"US CPI", utc(2026, 1, 14, 13, 30)},
	{"US CPI", utc(2026, 2, 11, 13, 30)},
	{"US CPI", utc(2026, 3, 11, 13, 30)},
	{"US CPI", utc(2026, 4, 8, 13, 30)},
	{"US CPI", utc(2026, 5, 12, 13, 30)},
	{"US CPI", utc(2026, 6, 10, 13, 30)},
	{"US CPI", utc(2026, 7, 9, 13, 30)},
	{"US CPI", utc(2026, 8, 11, 13, 30)},
	{"US CPI", utc(2026, 9, 9, 13, 30)},
	{"US CPI", utc(2026, 10, 8, 13, 30)},
	{"US CPI", utc(2026, 11, 12, 13, 30)},
	{"US CPI", utc(2026, 12, 9, 13, 30)},
}

// ─── Non-Farm Payroll ─────────────────────────────────────────────────────────
// NFP dirilis setiap Jumat pertama tiap bulan jam 13:30 UTC.
// Dihitung dinamis untuk tahun berjalan dan tahun berikutnya.

func nfpEvents(year int) []newsEvent {
	var events []newsEvent
	for m := time.January; m <= time.December; m++ {
		t := firstFriday(year, m)
		t = time.Date(t.Year(), t.Month(), t.Day(), 13, 30, 0, 0, time.UTC)
		events = append(events, newsEvent{"US Non-Farm Payroll", t})
	}
	return events
}

// firstFriday mengembalikan Jumat pertama pada bulan dan tahun yang diberikan.
func firstFriday(year int, month time.Month) time.Time {
	t := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	for t.Weekday() != time.Friday {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// ─── Public API ───────────────────────────────────────────────────────────────

// IsNewsTime mengembalikan true jika waktu UTC saat ini berada dalam jendela
// ±15 menit dari salah satu event berita berdampak tinggi:
//   - US Non-Farm Payroll (Jumat pertama tiap bulan, 13:30 UTC)
//   - FOMC Rate Decision (8x/tahun, 19:00 UTC)
//   - US CPI (bulanan, 13:30 UTC)
//
// Jika true, bot harus melewatkan semua order entry.
func IsNewsTime() bool {
	now := time.Now().UTC()
	return isNearAny(now, allEvents(now.Year()))
}

// ActiveNewsName mengembalikan nama event yang sedang aktif (jika ada), atau string kosong.
func ActiveNewsName() string {
	now := time.Now().UTC()
	for _, ev := range allEvents(now.Year()) {
		diff := now.Sub(ev.t)
		if diff < 0 {
			diff = -diff
		}
		if diff <= blackoutWindow {
			return ev.name
		}
	}
	return ""
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func allEvents(year int) []newsEvent {
	events := nfpEvents(year)
	events = append(events, nfpEvents(year+1)...)
	events = append(events, fomcDates2025...)
	events = append(events, fomcDates2026...)
	events = append(events, cpiDates2025...)
	events = append(events, cpiDates2026...)
	return events
}

func isNearAny(now time.Time, events []newsEvent) bool {
	for _, ev := range events {
		diff := now.Sub(ev.t)
		if diff < 0 {
			diff = -diff
		}
		if diff <= blackoutWindow {
			return true
		}
	}
	return false
}

func utc(year int, month time.Month, day, hour, min int) time.Time {
	return time.Date(year, month, day, hour, min, 0, 0, time.UTC)
}
