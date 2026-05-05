package mt5

import (
	"fmt"
	"math"
	"time"
)

const (
	defaultMinLot = 0.01
	defaultLotStep = 0.01
	// News blackout: 13:30 – 14:30 UTC (high-impact economic news window).
	newsBlackoutStartMin = 13*60 + 30
	newsBlackoutEndMin   = 14*60 + 30
)

// ValidateOrder memeriksa apakah order valid sebelum dikirim ke MT5.
//
//   symbol          – pasangan mata uang (e.g. "EURUSD")
//   volume          – ukuran lot yang akan dibuka
//   marginRequired  – estimasi margin yang dibutuhkan (dalam currency akun)
//   acc             – data akun live dari MT5
//
// Return error berisi alasan detail jika order ditolak, nil jika lolos semua cek.
func ValidateOrder(symbol string, volume float64, marginRequired float64, acc *AccountInfo) error {
	if acc == nil {
		return fmt.Errorf("data akun tidak tersedia — belum terhubung ke MT5")
	}

	// ── 1. Cek margin cukup: margin_required < free_margin × 0.8 ─────────────
	safeMargin := acc.FreeMargin * 0.8
	if marginRequired >= safeMargin {
		return fmt.Errorf(
			"margin tidak cukup: dibutuhkan %.2f, batas aman %.2f (80%% dari free_margin %.2f)",
			marginRequired, safeMargin, acc.FreeMargin,
		)
	}

	// ── 2. Cek volume >= minimal lot ──────────────────────────────────────────
	if volume < defaultMinLot {
		return fmt.Errorf(
			"volume %.4f di bawah minimal lot %.2f untuk %s",
			volume, defaultMinLot, symbol,
		)
	}

	// ── 3. Cek volume adalah kelipatan lot_step ───────────────────────────────
	steps := volume / defaultLotStep
	if math.Abs(steps-math.Round(steps)) > 1e-9 {
		return fmt.Errorf(
			"volume %.4f bukan kelipatan lot step %.2f",
			volume, defaultLotStep,
		)
	}

	// ── 4. Cek jam news blackout: 13:30 – 14:30 UTC ──────────────────────────
	now := time.Now().UTC()
	minsSinceMidnight := now.Hour()*60 + now.Minute()
	if minsSinceMidnight >= newsBlackoutStartMin && minsSinceMidnight < newsBlackoutEndMin {
		return fmt.Errorf(
			"order ditolak: jam news blackout 13:30–14:30 UTC (sekarang %02d:%02d UTC)",
			now.Hour(), now.Minute(),
		)
	}

	return nil
}
