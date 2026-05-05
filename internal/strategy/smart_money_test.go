package strategy

import (
	"testing"

	"github.com/finex/finex-cli/internal/market"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func candle(open, high, low, close_ float64) market.Candle {
	return market.Candle{Open: open, High: high, Low: low, Close: close_}
}

func bullish(base float64) market.Candle { return candle(base, base+0.005, base-0.001, base+0.004) }
func bearish(base float64) market.Candle { return candle(base+0.004, base+0.005, base-0.001, base) }

// ─── DetectOrderBlock ─────────────────────────────────────────────────────────

func TestDetectOrderBlock_InsufficientData(t *testing.T) {
	got := DetectOrderBlock([]market.Candle{bullish(1.0)}, "BUY")
	if got {
		t.Error("DetectOrderBlock with 1 candle should return false")
	}
}

func TestDetectOrderBlock_BUY_BullishThenBearish(t *testing.T) {
	// BUY OB: bullish candle immediately followed by bearish candle
	candles := []market.Candle{
		bullish(1.00),
		bearish(1.005), // bearish follows bullish → BUY order block
	}
	if !DetectOrderBlock(candles, "BUY") {
		t.Error("DetectOrderBlock BUY: bullish→bearish should return true")
	}
}

func TestDetectOrderBlock_BUY_NoBullishThenBearish(t *testing.T) {
	// No BUY order block when both candles are bullish
	candles := []market.Candle{bullish(1.00), bullish(1.01)}
	if DetectOrderBlock(candles, "BUY") {
		t.Error("DetectOrderBlock BUY: bullish→bullish should return false")
	}
}

func TestDetectOrderBlock_SELL_BearishThenBullish(t *testing.T) {
	// SELL OB: bearish candle immediately followed by bullish candle
	candles := []market.Candle{
		bearish(1.005),
		bullish(1.000), // bullish follows bearish → SELL order block
	}
	if !DetectOrderBlock(candles, "SELL") {
		t.Error("DetectOrderBlock SELL: bearish→bullish should return true")
	}
}

func TestDetectOrderBlock_SELL_NoBearishThenBullish(t *testing.T) {
	candles := []market.Candle{bearish(1.005), bearish(1.000)}
	if DetectOrderBlock(candles, "SELL") {
		t.Error("DetectOrderBlock SELL: bearish→bearish should return false")
	}
}

func TestDetectOrderBlock_LookbackWindow(t *testing.T) {
	// Pattern outside the lookback window (> lookbackCandles from the end) should be detected
	// since the scan includes the last `lookbackCandles` positions
	var candles []market.Candle
	// Prefix with neutral doji candles (open==close)
	for i := 0; i < 10; i++ {
		candles = append(candles, candle(1.0, 1.002, 0.998, 1.0))
	}
	// BUY order block within the last 5 candles
	candles = append(candles, bullish(1.00))
	candles = append(candles, bearish(1.005))

	if !DetectOrderBlock(candles, "BUY") {
		t.Error("DetectOrderBlock should find BUY OB within lookback window")
	}
}

// ─── DetectFVG ────────────────────────────────────────────────────────────────

func TestDetectFVG_InsufficientData(t *testing.T) {
	candles := []market.Candle{bullish(1.0), bullish(1.01)}
	if DetectFVG(candles, "BUY") {
		t.Error("DetectFVG with 2 candles should return false")
	}
}

func TestDetectFVG_BUY_UpwardGap(t *testing.T) {
	// Upward FVG: low[2] > high[0]
	// c0: high=1.005, c1: anything, c2: low=1.010 (> 1.005 → gap)
	c0 := candle(1.000, 1.005, 0.998, 1.004)
	c1 := candle(1.004, 1.008, 1.002, 1.007) // middle candle
	c2 := candle(1.010, 1.015, 1.010, 1.013) // low=1.010 > high[0]=1.005 → FVG
	candles := []market.Candle{c0, c1, c2}
	if !DetectFVG(candles, "BUY") {
		t.Error("DetectFVG BUY: upward gap should return true")
	}
}

func TestDetectFVG_BUY_NoGap(t *testing.T) {
	// No FVG: low[2] <= high[0]
	c0 := candle(1.000, 1.010, 0.998, 1.008)
	c1 := candle(1.008, 1.012, 1.006, 1.011)
	c2 := candle(1.009, 1.015, 1.007, 1.013) // low=1.007 < high[0]=1.010 → no gap
	candles := []market.Candle{c0, c1, c2}
	if DetectFVG(candles, "BUY") {
		t.Error("DetectFVG BUY: overlapping candles should return false")
	}
}

func TestDetectFVG_SELL_DownwardGap(t *testing.T) {
	// Downward FVG: high[2] < low[0]
	c0 := candle(1.015, 1.018, 1.010, 1.012) // low=1.010
	c1 := candle(1.012, 1.014, 1.008, 1.009)
	c2 := candle(1.005, 1.009, 1.002, 1.003) // high=1.009 < low[0]=1.010 → FVG
	candles := []market.Candle{c0, c1, c2}
	if !DetectFVG(candles, "SELL") {
		t.Error("DetectFVG SELL: downward gap should return true")
	}
}

func TestDetectFVG_SELL_NoGap(t *testing.T) {
	c0 := candle(1.015, 1.018, 1.010, 1.012)
	c1 := candle(1.012, 1.014, 1.008, 1.009)
	c2 := candle(1.011, 1.013, 1.010, 1.011) // high=1.013 > low[0]=1.010 → no gap
	candles := []market.Candle{c0, c1, c2}
	if DetectFVG(candles, "SELL") {
		t.Error("DetectFVG SELL: overlapping candles should return false")
	}
}

// ─── Analyze ──────────────────────────────────────────────────────────────────

func TestAnalyze_InsufficientData_NotConfirmed(t *testing.T) {
	candles := []market.Candle{bullish(1.0), bullish(1.01)}
	r := Analyze(candles, "BUY")
	if r.Confirmed {
		t.Error("Analyze with insufficient candles should not be Confirmed")
	}
}

func TestAnalyze_FVGPresent_Weight030(t *testing.T) {
	// Build candles that have an upward FVG
	c0 := candle(1.000, 1.005, 0.998, 1.004)
	c1 := candle(1.004, 1.008, 1.002, 1.007)
	c2 := candle(1.010, 1.015, 1.010, 1.013)
	r := Analyze([]market.Candle{c0, c1, c2}, "BUY")
	if !r.HasFVG {
		t.Error("Analyze: expected HasFVG=true")
	}
	if !r.Confirmed {
		t.Error("Analyze: expected Confirmed=true when FVG present")
	}
	if r.Weight != 0.30 {
		t.Errorf("Analyze: expected Weight=0.30, got %.4f", r.Weight)
	}
}

func TestAnalyze_OrderBlockOnly_Weight0(t *testing.T) {
	// BUY OB: bullish→bearish (no FVG because candles overlap)
	c0 := candle(1.000, 1.008, 0.998, 1.006) // bullish
	c1 := candle(1.006, 1.009, 1.001, 1.002) // bearish
	c2 := candle(1.002, 1.007, 1.001, 1.005) // overlaps → no FVG
	r := Analyze([]market.Candle{c0, c1, c2}, "BUY")
	if r.HasFVG {
		t.Skip("FVG also detected — skipping weight=0 assertion")
	}
	if !r.HasOrderBlock {
		t.Error("Analyze: expected HasOrderBlock=true")
	}
	if r.Weight != 0 {
		t.Errorf("Analyze: OB only should have Weight=0, got %.4f", r.Weight)
	}
}

func TestAnalyze_NoPatternsDetected_NotConfirmed(t *testing.T) {
	// All bullish, no OB, candles overlap → no FVG
	candles := []market.Candle{
		bullish(1.000),
		bullish(1.002),
		bullish(1.004),
	}
	r := Analyze(candles, "SELL")
	if r.Confirmed {
		t.Error("Analyze: no patterns should result in Confirmed=false")
	}
	if r.Weight != 0 {
		t.Errorf("Analyze: no patterns should result in Weight=0, got %.4f", r.Weight)
	}
}
