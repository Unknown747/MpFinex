package bot

import "github.com/finex/finex-cli/internal/indicator"

// PipSizes memetakan setiap simbol ke ukuran pip-nya (pergerakan harga untuk 1 pip).
var PipSizes = map[string]float64{
	"EURUSD": 0.0001,
	"GBPUSD": 0.0001,
	"USDJPY": 0.01,
	"AUDUSD": 0.0001,
	"USDCAD": 0.0001,
	"USDCHF": 0.0001,
	"EURGBP": 0.0001,
	"EURJPY": 0.01,
}

const (
	breakevenPips = 20.0 // profit (pip) untuk aktifkan breakeven
	trailingPips  = 30.0 // profit (pip) untuk aktifkan trailing stop
	trailingDist  = 15.0 // jarak trailing stop (pip) dari harga saat ini
)

// UpdateTrailingStop memperbarui SLPrice trade aktif setiap tick berdasarkan dua aturan:
// Juga menginkremen TicksSinceOpen setiap kali dipanggil (1 tick = 1 increment).
//
//  1. Breakeven: setelah profit ≥ 20 pip → pindahkan SL ke harga entry.
//  2. Trailing stop: setelah profit ≥ 30 pip → aktifkan trailing stop jarak 15 pip.
//
// Fungsi ini memodifikasi Trade in-place dan aman dipanggil berkali-kali per tick.
func UpdateTrailingStop(trade *Trade, currentPrice float64, symbol string) {
	if trade == nil || trade.Status == Closed {
		return
	}

	pipSize := PipSizes[symbol]
	if pipSize == 0 {
		pipSize = 0.0001
	}

	trade.TicksSinceOpen++

	entry := trade.EntryPrice
	var profitPips float64
	if trade.Side == Buy {
		profitPips = (currentPrice - entry) / pipSize
	} else {
		profitPips = (entry - currentPrice) / pipSize
	}

	// Step 1: Breakeven — pindahkan SL ke entry setelah 20 pip profit
	if !trade.BreakevenSet && profitPips >= breakevenPips {
		trade.SLPrice = entry
		trade.BreakevenSet = true
	}

	// Step 2: Trailing stop — aktifkan setelah 30 pip profit, jarak 15 pip
	if profitPips >= trailingPips {
		trailDist := trailingDist * pipSize
		if trade.Side == Buy {
			newSL := currentPrice - trailDist
			if !trade.TrailingActive || newSL > trade.SLPrice {
				trade.SLPrice = newSL
				trade.TrailingActive = true
			}
		} else {
			newSL := currentPrice + trailDist
			if !trade.TrailingActive || newSL < trade.SLPrice {
				trade.SLPrice = newSL
				trade.TrailingActive = true
			}
		}
	}
}

// ─── RSI Divergence Monitor ───────────────────────────────────────────────────

// MonitorDivergence mendeteksi RSI divergence untuk triggering exit trade lebih awal.
// Hanya melakukan scan setiap 10 tick (ticksSinceOpen % 10 == 0) untuk menghemat CPU.
//
//	direction == "BUY"  → cari hidden bullish divergence (harga LL, RSI HL) → exit BUY
//	direction == "SELL" → cari hidden bearish divergence (harga HH, RSI LH) → exit SELL
//
// Return true jika divergence terdeteksi (sinyal untuk tutup posisi lebih awal).
func MonitorDivergence(highs, lows, closes []float64, direction string, ticksSinceOpen int) bool {
	// Scan hanya setiap 10 tick
	if ticksSinceOpen == 0 || ticksSinceOpen%10 != 0 {
		return false
	}

	n := len(closes)
	if n < 20 || len(highs) < n || len(lows) < n {
		return false // data tidak cukup
	}

	// Ambil 20 data terakhir, split menjadi dua paruh:
	// paruh lama [recentStart:mid] vs paruh baru [mid:n]
	recentStart := n - 20
	mid := n - 10

	switch direction {
	case "BUY":
		// Hidden bullish divergence: harga lower low TAPI RSI higher low → exit BUY lebih awal
		recentLow := minSlice(lows[mid:])
		olderLow := minSlice(lows[recentStart:mid])
		if recentLow >= olderLow {
			return false // tidak ada lower low di harga
		}
		rsiRecent := indicator.RSI(closes[mid:], 7)
		rsiOlder := indicator.RSI(closes[recentStart:mid], 7)
		return rsiRecent > rsiOlder // RSI higher low = bullish divergence

	case "SELL":
		// Hidden bearish divergence: harga higher high TAPI RSI lower high → exit SELL lebih awal
		recentHigh := maxSlice(highs[mid:])
		olderHigh := maxSlice(highs[recentStart:mid])
		if recentHigh <= olderHigh {
			return false // tidak ada higher high di harga
		}
		rsiRecent := indicator.RSI(closes[mid:], 7)
		rsiOlder := indicator.RSI(closes[recentStart:mid], 7)
		return rsiRecent < rsiOlder // RSI lower high = bearish divergence
	}
	return false
}

// minSlice mengembalikan nilai minimum dari slice float64.
func minSlice(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := s[0]
	for _, v := range s[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

// maxSlice mengembalikan nilai maksimum dari slice float64.
func maxSlice(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
