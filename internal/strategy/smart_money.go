// Package strategy menyediakan analisis smart money concepts (SMC) meliputi
// Order Block dan Fair Value Gap (FVG) yang digunakan sebagai filter konfirmasi
// sebelum eksekusi order entry.
package strategy

import "github.com/finex/finex-cli/internal/market"

// SmartMoneyResult menyimpan hasil analisis SMC untuk satu sinyal.
type SmartMoneyResult struct {
	HasOrderBlock bool    // true jika ada order block dalam lookback window
	HasFVG        bool    // true jika ada Fair Value Gap dalam lookback window
	Weight        float64 // bobot sinyal tambahan: 0.0 normal, 0.30 jika ada FVG
	Confirmed     bool    // true jika minimal satu pola terdeteksi
}

// lookbackCandles adalah jumlah candle terakhir yang discan untuk pattern SMC.
const lookbackCandles = 5

// Analyze memeriksa Order Block dan FVG pada `candles` terakhir.
// direction harus "BUY" atau "SELL".
//
// Mengembalikan SmartMoneyResult; jika candle tidak cukup, Confirmed=false.
func Analyze(candles []market.Candle, direction string) SmartMoneyResult {
	var r SmartMoneyResult
	if len(candles) < 3 {
		return r
	}
	r.HasOrderBlock = DetectOrderBlock(candles, direction)
	r.HasFVG = DetectFVG(candles, direction)
	r.Confirmed = r.HasOrderBlock || r.HasFVG
	if r.HasFVG {
		r.Weight = 0.30 // FVG memberikan konfirmasi lebih kuat
	}
	return r
}

// ─── Order Block ──────────────────────────────────────────────────────────────

// DetectOrderBlock mendeteksi pola Order Block dalam `lookbackCandles` terakhir.
//
// Definisi:
//   - BUY Order Block : candle bullish diikuti candle bearish segera sesudahnya.
//     Level entry = low candle bullish (demand zone).
//   - SELL Order Block: candle bearish diikuti candle bullish segera sesudahnya.
//     Level entry = high candle bearish (supply zone).
func DetectOrderBlock(candles []market.Candle, direction string) bool {
	n := len(candles)
	if n < 2 {
		return false
	}
	start := n - lookbackCandles
	if start < 0 {
		start = 0
	}
	// Pastikan ada ruang untuk melihat candle berikutnya (i+1)
	for i := start; i < n-1; i++ {
		curr := candles[i]
		next := candles[i+1]

		isCurrBullish := curr.Close > curr.Open
		isCurrBearish := curr.Close < curr.Open
		isNextBullish := next.Close > next.Open
		isNextBearish := next.Close < next.Open

		switch direction {
		case "BUY":
			// Bullish OB: candle bullish diikuti candle bearish
			if isCurrBullish && isNextBearish {
				return true
			}
		case "SELL":
			// Bearish OB: candle bearish diikuti candle bullish
			if isCurrBearish && isNextBullish {
				return true
			}
		}
	}
	return false
}

// ─── Fair Value Gap (FVG) ─────────────────────────────────────────────────────

// DetectFVG mendeteksi Fair Value Gap dalam `lookbackCandles` terakhir.
//
// FVG terbentuk saat ada gap (imbalance) antara candle ke-0 dan candle ke-2
// dari tiga candle berurutan:
//   - Upward FVG  : Low[2] > High[0]  — permintaan kuat (BUY zone)
//   - Downward FVG: High[2] < Low[0]  — penawaran kuat (SELL zone)
func DetectFVG(candles []market.Candle, direction string) bool {
	n := len(candles)
	if n < 3 {
		return false
	}
	start := n - lookbackCandles
	if start < 0 {
		start = 0
	}
	for i := start; i+2 < n; i++ {
		c0 := candles[i]
		c2 := candles[i+2]

		switch direction {
		case "BUY":
			// Upward FVG: low candle ke-2 berada di atas high candle ke-0
			if c2.Low > c0.High {
				return true
			}
		case "SELL":
			// Downward FVG: high candle ke-2 berada di bawah low candle ke-0
			if c2.High < c0.Low {
				return true
			}
		}
	}
	return false
}
