package indicator

import (
	"math"
	"testing"
	"time"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func approx(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// linspace returns n evenly spaced values from start to end (inclusive).
func linspace(start, end float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = start + (end-start)*float64(i)/float64(n-1)
	}
	return out
}

// ─── RSI ──────────────────────────────────────────────────────────────────────

func TestRSI_InsufficientData(t *testing.T) {
	closes := []float64{1.0, 1.1} // need period+1 = 15 minimum for period=14
	got := RSI(closes, 14)
	if got != 50 {
		t.Errorf("RSI with insufficient data: want 50, got %.2f", got)
	}
}

func TestRSI_AllGains_Returns100(t *testing.T) {
	// Strictly increasing series — all gains, no losses → RSI ≈ 100
	closes := linspace(1.0, 2.0, 30)
	got := RSI(closes, 14)
	if got < 99 {
		t.Errorf("RSI all-gains: want ~100, got %.2f", got)
	}
}

func TestRSI_AllLosses_Returns0(t *testing.T) {
	// Strictly decreasing series — all losses, no gains → RSI ≈ 0
	closes := linspace(2.0, 1.0, 30)
	got := RSI(closes, 14)
	if got > 1 {
		t.Errorf("RSI all-losses: want ~0, got %.2f", got)
	}
}

func TestRSI_Range(t *testing.T) {
	closes := []float64{
		1.0, 1.05, 1.03, 1.07, 1.06, 1.09, 1.08, 1.11,
		1.10, 1.13, 1.12, 1.15, 1.14, 1.17, 1.16, 1.19,
		1.18, 1.21, 1.20, 1.23, 1.22, 1.25, 1.24, 1.27,
		1.26, 1.29, 1.28, 1.31, 1.30, 1.33,
	}
	got := RSI(closes, 14)
	if got < 0 || got > 100 {
		t.Errorf("RSI out of [0,100]: got %.2f", got)
	}
}

// ─── EMA ──────────────────────────────────────────────────────────────────────

func TestEMA_InsufficientData(t *testing.T) {
	got := EMA([]float64{1.0, 2.0}, 5)
	if got != 0 {
		t.Errorf("EMA insufficient data: want 0, got %.4f", got)
	}
}

func TestEMA_ConstantSeries(t *testing.T) {
	// EMA of a constant series should equal that constant
	const c = 1.5
	closes := make([]float64, 20)
	for i := range closes {
		closes[i] = c
	}
	got := EMA(closes, 10)
	if !approx(got, c, 1e-9) {
		t.Errorf("EMA constant series: want %.4f, got %.4f", c, got)
	}
}

func TestEMA_ReactsToRecentPrices(t *testing.T) {
	// EMA should weight recent prices more than SMA
	closes := make([]float64, 20)
	for i := range closes {
		closes[i] = 1.0
	}
	closes[19] = 10.0 // spike on last candle

	ema := EMA(closes, 10)
	sma := SMA(closes, 10)
	// EMA should be pulled up more than SMA toward the spike
	if ema <= sma {
		t.Errorf("EMA should react more to recent spike than SMA: EMA=%.4f SMA=%.4f", ema, sma)
	}
}

// ─── SMA ──────────────────────────────────────────────────────────────────────

func TestSMA_InsufficientData(t *testing.T) {
	got := SMA([]float64{1.0, 2.0}, 5)
	if got != 0 {
		t.Errorf("SMA insufficient data: want 0, got %.4f", got)
	}
}

func TestSMA_ExactPeriod(t *testing.T) {
	closes := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	got := SMA(closes, 5)
	if !approx(got, 3.0, 1e-9) {
		t.Errorf("SMA exact period: want 3.0, got %.4f", got)
	}
}

func TestSMA_UsesLastNCandles(t *testing.T) {
	// SMA(period=3) should only use the last 3 elements
	closes := []float64{100.0, 100.0, 1.0, 2.0, 3.0}
	got := SMA(closes, 3)
	if !approx(got, 2.0, 1e-9) {
		t.Errorf("SMA last-N: want 2.0, got %.4f", got)
	}
}

// ─── Bollinger Bands ──────────────────────────────────────────────────────────

func TestBollingerBands_InsufficientData(t *testing.T) {
	mid, upper, lower := BollingerBands([]float64{1.0, 2.0}, 5, 2.0)
	if mid != 0 || upper != 0 || lower != 0 {
		t.Errorf("BB insufficient data: want (0,0,0), got (%.4f,%.4f,%.4f)", mid, upper, lower)
	}
}

func TestBollingerBands_ConstantSeries(t *testing.T) {
	// Constant series → stddev = 0 → all three bands equal the constant
	closes := make([]float64, 20)
	for i := range closes {
		closes[i] = 1.23
	}
	mid, upper, lower := BollingerBands(closes, 20, 2.0)
	if !approx(mid, 1.23, 1e-9) || !approx(upper, 1.23, 1e-9) || !approx(lower, 1.23, 1e-9) {
		t.Errorf("BB constant series: want (1.23,1.23,1.23), got (%.4f,%.4f,%.4f)", mid, upper, lower)
	}
}

func TestBollingerBands_Ordering(t *testing.T) {
	closes := []float64{
		1.0, 1.05, 1.03, 1.07, 1.06, 1.09, 1.08, 1.11,
		1.10, 1.13, 1.12, 1.15, 1.14, 1.17, 1.16, 1.19,
		1.18, 1.21, 1.20, 1.23,
	}
	mid, upper, lower := BollingerBands(closes, 20, 2.0)
	if upper <= mid {
		t.Errorf("BB: upper should be > mid; upper=%.4f mid=%.4f", upper, mid)
	}
	if lower >= mid {
		t.Errorf("BB: lower should be < mid; lower=%.4f mid=%.4f", lower, mid)
	}
}

// ─── ATR ──────────────────────────────────────────────────────────────────────

func makeOHLC(n int, base, range_ float64) (highs, lows, closes []float64) {
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	for i := range closes {
		closes[i] = base
		highs[i] = base + range_
		lows[i] = base - range_
	}
	return
}

func TestATR_InsufficientData(t *testing.T) {
	h, l, c := makeOHLC(5, 1.0, 0.01)
	got := ATR(h, l, c, 14)
	if got != 0 {
		t.Errorf("ATR insufficient data: want 0, got %.6f", got)
	}
}

func TestATR_ConstantCandles(t *testing.T) {
	// If each candle has the same HL range and no gap from previous close,
	// ATR should converge to that range (2×range_ = high-low).
	h, l, c := makeOHLC(30, 1.5000, 0.0050)
	got := ATR(h, l, c, 14)
	if !approx(got, 0.01, 1e-4) {
		t.Errorf("ATR constant candles: want ~0.01, got %.6f", got)
	}
}

func TestATR_Positive(t *testing.T) {
	h, l, c := makeOHLC(30, 1.0, 0.005)
	got := ATR(h, l, c, 14)
	if got <= 0 {
		t.Errorf("ATR should be > 0, got %.6f", got)
	}
}

// ─── ADX ──────────────────────────────────────────────────────────────────────

func TestADX_InsufficientData(t *testing.T) {
	h, l, c := makeOHLC(10, 1.0, 0.005)
	got := ADX(h, l, c, 14) // need 2*14+1 = 29 candles
	if got != 0 {
		t.Errorf("ADX insufficient data: want 0, got %.4f", got)
	}
}

func TestADX_StrongTrend(t *testing.T) {
	// Uniformly rising market — each candle is higher and non-overlapping
	n := 60
	highs := make([]float64, n)
	lows := make([]float64, n)
	closes := make([]float64, n)
	for i := 0; i < n; i++ {
		base := float64(i) * 0.01
		highs[i] = 1.0 + base + 0.005
		lows[i] = 1.0 + base
		closes[i] = 1.0 + base + 0.003
	}
	got := ADX(highs, lows, closes, 14)
	if got <= 0 {
		t.Errorf("ADX strong trend: want > 0, got %.4f", got)
	}
}

func TestADX_Range(t *testing.T) {
	h, l, c := makeOHLC(60, 1.0, 0.005)
	got := ADX(h, l, c, 14)
	if got < 0 || got > 100 {
		t.Errorf("ADX out of [0,100]: got %.4f", got)
	}
}

// ─── Indicator Cache ──────────────────────────────────────────────────────────

func TestCache_MissAndHit(t *testing.T) {
	key := "test_cache_key_xyz"
	_, ok := GetCachedIndicator(key)
	if ok {
		t.Fatal("Cache hit before Set — expected miss")
	}

	SetCachedIndicator(key, 42.5)
	val, ok := GetCachedIndicator(key)
	if !ok {
		t.Fatal("Cache miss after Set — expected hit")
	}
	if !approx(val, 42.5, 1e-9) {
		t.Errorf("Cache value: want 42.5, got %.6f", val)
	}
}

func TestCache_Expiry(t *testing.T) {
	oldTTL := cacheTTL
	cacheTTL = 1 // 1 nanosecond — expires instantly
	defer func() { cacheTTL = oldTTL }()

	key := "test_cache_expiry"
	SetCachedIndicator(key, 99.9)
	time.Sleep(2 * time.Millisecond)

	_, ok := GetCachedIndicator(key)
	if ok {
		t.Error("Cache should have expired but returned a hit")
	}
}
