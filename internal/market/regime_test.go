package market

import (
	"testing"
)

// makeCandles builds synthetic candle data for testing.
// direction: +1 = trending up, -1 = trending down, 0 = ranging (random walk)
func makeCandles(n int, basePrice, volatility float64, direction int) (closes, highs, lows []float64) {
	closes = make([]float64, n)
	highs = make([]float64, n)
	lows = make([]float64, n)
	p := basePrice
	for i := 0; i < n; i++ {
		var move float64
		switch direction {
		case 1:
			move = volatility * p * 0.6 // consistent uptrend
		case -1:
			move = -volatility * p * 0.6 // consistent downtrend
		default:
			// Alternate up/down for ranging
			if i%2 == 0 {
				move = volatility * p * 0.1
			} else {
				move = -volatility * p * 0.1
			}
		}
		p += move
		if p < 0.001 {
			p = 0.001
		}
		closes[i] = p
		highs[i] = p + volatility*p*0.2
		lows[i] = p - volatility*p*0.2
		if lows[i] < 0.001 {
			lows[i] = 0.001
		}
	}
	return
}

func TestRegimeString(t *testing.T) {
	tests := []struct {
		regime Regime
		want   string
	}{
		{RegimeUnknown, "UNKNOWN"},
		{RegimeTrending, "TRENDING"},
		{RegimeRanging, "RANGING"},
		{RegimeVolatile, "VOLATILE"},
	}
	for _, tc := range tests {
		if got := tc.regime.String(); got != tc.want {
			t.Errorf("Regime(%d).String() = %q, want %q", tc.regime, got, tc.want)
		}
	}
}

func TestDetectRegime_InsufficientData(t *testing.T) {
	closes, highs, lows := makeCandles(30, 1.085, 0.001, 0)
	regime, _, _ := detectRegime(closes, highs, lows)
	if regime != RegimeUnknown {
		t.Errorf("expected RegimeUnknown with only 30 candles, got %s", regime)
	}
}

func TestDetectRegime_NotUnknown_WithEnoughData(t *testing.T) {
	// 100 candles is enough for any regime classification.
	closes, highs, lows := makeCandles(100, 1.085, 0.001, 0)
	regime, _, _ := detectRegime(closes, highs, lows)
	if regime == RegimeUnknown {
		t.Error("expected a classified regime with 100 candles, got UNKNOWN")
	}
}

func TestRiskMultiplier(t *testing.T) {
	tests := []struct {
		regime Regime
		want   float64
	}{
		{RegimeVolatile, 0.5},
		{RegimeTrending, 1.0},
		{RegimeRanging, 1.0},
		{RegimeUnknown, 1.0},
	}
	for _, tc := range tests {
		got := RiskMultiplier(tc.regime)
		if got != tc.want {
			t.Errorf("RiskMultiplier(%s) = %.2f, want %.2f", tc.regime, got, tc.want)
		}
	}
}

func TestStrategyWeight_Trending(t *testing.T) {
	tests := []struct {
		strategy string
		want     float64
	}{
		{"Trend Following", 0.70},
		{"Swing Trading", 0.70},
		{"Scalping", 0.30},
		{"Mean Reversion", 0.30},
	}
	for _, tc := range tests {
		got := StrategyWeight(RegimeTrending, tc.strategy)
		if got != tc.want {
			t.Errorf("StrategyWeight(TRENDING, %q) = %.2f, want %.2f",
				tc.strategy, got, tc.want)
		}
	}
}

func TestStrategyWeight_Ranging(t *testing.T) {
	tests := []struct {
		strategy string
		want     float64
	}{
		{"Mean Reversion", 0.70},
		{"Scalping", 0.70},
		{"Trend Following", 0.30},
		{"Swing Trading", 0.30},
	}
	for _, tc := range tests {
		got := StrategyWeight(RegimeRanging, tc.strategy)
		if got != tc.want {
			t.Errorf("StrategyWeight(RANGING, %q) = %.2f, want %.2f",
				tc.strategy, got, tc.want)
		}
	}
}

func TestStrategyWeight_Volatile(t *testing.T) {
	for _, s := range []string{"Scalping", "Swing Trading", "Trend Following", "Mean Reversion"} {
		got := StrategyWeight(RegimeVolatile, s)
		if got != 0.50 {
			t.Errorf("StrategyWeight(VOLATILE, %q) = %.2f, want 0.50", s, got)
		}
	}
}

func TestStrategyWeight_Unknown(t *testing.T) {
	for _, s := range []string{"Scalping", "Swing Trading"} {
		got := StrategyWeight(RegimeUnknown, s)
		if got != 1.0 {
			t.Errorf("StrategyWeight(UNKNOWN, %q) = %.2f, want 1.0", s, got)
		}
	}
}

func TestRegimeDetector_GetCached_BeforeDetect(t *testing.T) {
	rd := NewRegimeDetector()
	if got := rd.GetCached("EURUSD"); got != RegimeUnknown {
		t.Errorf("GetCached before any detection: expected UNKNOWN, got %s", got)
	}
}

func TestRegimeDetector_ForceUpdate(t *testing.T) {
	mkt := NewMarket()
	rd := NewRegimeDetector()
	regime := rd.ForceUpdate("EURUSD", mkt)
	// After force update, GetCached must match.
	if got := rd.GetCached("EURUSD"); got != regime {
		t.Errorf("GetCached() = %s, ForceUpdate() returned %s — mismatch", got, regime)
	}
}

func TestComputeMeanATR_InsufficientData(t *testing.T) {
	closes, highs, lows := makeCandles(10, 1.0, 0.001, 0)
	mean := computeMeanATR(highs, lows, closes, 14, 50)
	if mean != 0 {
		t.Errorf("expected 0 for insufficient data, got %.6f", mean)
	}
}

func TestComputeMeanATR_SufficientData(t *testing.T) {
	closes, highs, lows := makeCandles(100, 1.085, 0.001, 0)
	mean := computeMeanATR(highs, lows, closes, 14, 50)
	if mean <= 0 {
		t.Errorf("expected positive mean ATR with 100 candles, got %.6f", mean)
	}
}
