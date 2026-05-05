package bot

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
