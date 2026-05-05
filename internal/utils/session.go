package utils

import "time"

// Sesi trading forex berdasarkan UTC:
//   London: 07:00–16:00 UTC (likuiditas tinggi, spread rendah)
//   New York: 13:00–22:00 UTC (volume terbesar, overlap London 13:00–16:00)
//
// Di luar window ini (sesi Asia 22:00–07:00 UTC) spread melebar,
// volatilitas rendah, dan sinyal sering palsu.

const (
	londonOpenMin  = 7 * 60  // 07:00 UTC
	londonCloseMin = 16 * 60 // 16:00 UTC
	nyOpenMin      = 13 * 60 // 13:00 UTC
	nyCloseMin     = 22 * 60 // 22:00 UTC
)

// IsActiveSession mengembalikan true jika waktu UTC saat ini berada dalam
// sesi London (07:00–16:00) atau New York (13:00–22:00).
// Kembalikan false di sesi Asia (22:00–07:00 UTC) — bot tidak membuka posisi baru.
func IsActiveSession() bool {
	now := time.Now().UTC()
	mins := now.Hour()*60 + now.Minute()
	london := mins >= londonOpenMin && mins < londonCloseMin
	newYork := mins >= nyOpenMin && mins < nyCloseMin
	return london || newYork
}

// ActiveSessionName mengembalikan nama sesi aktif saat ini, atau "Asia" jika di luar sesi.
func ActiveSessionName() string {
	now := time.Now().UTC()
	mins := now.Hour()*60 + now.Minute()
	switch {
	case mins >= londonOpenMin && mins < londonCloseMin && mins >= nyOpenMin:
		return "London+NY"
	case mins >= londonOpenMin && mins < londonCloseMin:
		return "London"
	case mins >= nyOpenMin && mins < nyCloseMin:
		return "New York"
	default:
		return "Asia"
	}
}
