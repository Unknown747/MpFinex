// Package indicator provides technical analysis calculations
// used by trading strategies in Finex Bot.
package indicator

import (
	"math"
	"sync"
	"time"
)

// ─── Indicator Cache ──────────────────────────────────────────────────────────
//
// Shared cache with a 5-second TTL so bots on the same symbol reuse computed
// indicator values instead of recomputing on every tick.
// The goroutine pool in main.go pre-populates the cache before bot.Tick() runs.

type cacheEntry struct {
	val    float64
	expiry time.Time
}

var (
	indicatorCache sync.Map
	cacheTTL       = 5 * time.Second
)

// GetCachedIndicator retrieves a cached indicator value by key.
// Returns (value, true) if the entry exists and has not expired.
func GetCachedIndicator(key string) (float64, bool) {
	v, ok := indicatorCache.Load(key)
	if !ok {
		return 0, false
	}
	e := v.(cacheEntry)
	if time.Now().After(e.expiry) {
		indicatorCache.Delete(key)
		return 0, false
	}
	return e.val, true
}

// SetCachedIndicator stores a computed indicator value with a 5-second TTL.
// Called by the goroutine pool in main.go after each market tick.
func SetCachedIndicator(key string, value float64) {
	indicatorCache.Store(key, cacheEntry{val: value, expiry: time.Now().Add(cacheTTL)})
}

// ─── Slice pool ───────────────────────────────────────────────────────────────

// slicePool reuses float64 backing arrays for indicator workspace slices,
// reducing allocator pressure when 8 pairs × 4 bots tick at 500 ms.
var slicePool = sync.Pool{
	New: func() any { return make([]float64, 0, 256) },
}

// ─── RSI ─────────────────────────────────────────────────────────────────────

// RSI computes the Relative Strength Index for the given closes using the
// Wilder smoothing method. Returns 50 if there are not enough data points.
func RSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}

	var gains, losses float64
	for i := 1; i <= period; i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Wilder smoothing for remaining candles
	for i := period + 1; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			avgGain = (avgGain*float64(period-1) + diff) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - diff) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// ─── EMA ─────────────────────────────────────────────────────────────────────

// EMA computes the Exponential Moving Average over the given closes.
// Returns the last value. Returns 0 if not enough data.
func EMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	k := 2.0 / float64(period+1)
	ema := SMA(closes[:period], period)
	for i := period; i < len(closes); i++ {
		ema = closes[i]*k + ema*(1-k)
	}
	return ema
}

// ─── SMA ─────────────────────────────────────────────────────────────────────

// SMA computes the Simple Moving Average of the last `period` values.
func SMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	start := len(closes) - period
	sum := 0.0
	for _, v := range closes[start:] {
		sum += v
	}
	return sum / float64(period)
}

// ─── Bollinger Bands ─────────────────────────────────────────────────────────

// BollingerBands returns the middle (SMA), upper, and lower bands.
// Returns zeros if not enough data.
func BollingerBands(closes []float64, period int, stdMult float64) (mid, upper, lower float64) {
	if len(closes) < period {
		return 0, 0, 0
	}
	start := len(closes) - period
	slice := closes[start:]
	mid = 0
	for _, v := range slice {
		mid += v
	}
	mid /= float64(period)

	variance := 0.0
	for _, v := range slice {
		diff := v - mid
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	upper = mid + stdMult*stdDev
	lower = mid - stdMult*stdDev
	return mid, upper, lower
}

// ─── ATR ─────────────────────────────────────────────────────────────────────

// ATR (Average True Range) menghitung ATR menggunakan Wilder's smoothing.
// Membutuhkan highs, lows, closes dengan panjang yang sama (minimal period+1 elemen).
// Returns 0 jika data tidak cukup.
func ATR(highs, lows, closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 || len(highs) < n || len(lows) < n {
		return 0
	}

	// True Range untuk setiap candle mulai dari index 1.
	// Backing array diambil dari pool untuk mengurangi alokasi GC.
	trs := slicePool.Get().([]float64)[:0]
	defer func() { slicePool.Put(trs[:0]) }()
	for i := 1; i < n; i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr := hl
		if hc > tr {
			tr = hc
		}
		if lc > tr {
			tr = lc
		}
		trs = append(trs, tr)
	}

	if len(trs) < period {
		return 0
	}

	// Seed awal: SMA dari `period` TR pertama
	atr := 0.0
	for _, tr := range trs[:period] {
		atr += tr
	}
	atr /= float64(period)

	// Wilder smoothing untuk sisa TR
	for _, tr := range trs[period:] {
		atr = (atr*float64(period-1) + tr) / float64(period)
	}
	return atr
}

// ─── ADX ─────────────────────────────────────────────────────────────────────

// ADX computes the Average Directional Index using Wilder's smoothing method.
// Requires at least 2×period+1 candles in highs/lows/closes; returns 0 if data
// is insufficient. Typical interpretation: > 25 = trending, < 20 = ranging.
func ADX(highs, lows, closes []float64, period int) float64 {
	n := len(closes)
	if n < period*2+1 || len(highs) < n || len(lows) < n {
		return 0
	}

	plusDM := make([]float64, n-1)
	minusDM := make([]float64, n-1)
	trList := make([]float64, n-1)

	for i := 1; i < n; i++ {
		upMove := highs[i] - highs[i-1]
		downMove := lows[i-1] - lows[i]
		if upMove > downMove && upMove > 0 {
			plusDM[i-1] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i-1] = downMove
		}
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr := hl
		if hc > tr {
			tr = hc
		}
		if lc > tr {
			tr = lc
		}
		trList[i-1] = tr
	}

	// Seed: sum of first `period` DM/TR values.
	smoothTR, smoothPlus, smoothMinus := 0.0, 0.0, 0.0
	for i := 0; i < period && i < len(trList); i++ {
		smoothTR += trList[i]
		smoothPlus += plusDM[i]
		smoothMinus += minusDM[i]
	}

	// Build DX series using Wilder smoothing.
	dx := make([]float64, 0, len(trList)-period)
	for i := period; i < len(trList); i++ {
		smoothTR = smoothTR - smoothTR/float64(period) + trList[i]
		smoothPlus = smoothPlus - smoothPlus/float64(period) + plusDM[i]
		smoothMinus = smoothMinus - smoothMinus/float64(period) + minusDM[i]
		if smoothTR == 0 {
			continue
		}
		plusDI := 100 * smoothPlus / smoothTR
		minusDI := 100 * smoothMinus / smoothTR
		sum := plusDI + minusDI
		if sum == 0 {
			dx = append(dx, 0)
		} else {
			dx = append(dx, 100*math.Abs(plusDI-minusDI)/sum)
		}
	}

	if len(dx) < period {
		return 0
	}

	// ADX = Wilder smooth of the DX series.
	adx := 0.0
	for i := 0; i < period; i++ {
		adx += dx[i]
	}
	adx /= float64(period)
	for i := period; i < len(dx); i++ {
		adx = (adx*float64(period-1) + dx[i]) / float64(period)
	}
	return adx
}

// ─── Signal type ─────────────────────────────────────────────────────────────

// Signal represents a directional trade signal.
type Signal int

const (
	None  Signal = iota
	Long         // buy / go long
	Short        // sell / go short
)

// ─── Strategy signals ────────────────────────────────────────────────────────

// ScalpingSignal uses RSI(7): buy < 38, sell > 62.
// Thresholds are set at 38/62 (not the classic 30/70) because forex intraday
// volatility rarely pushes RSI to those extremes in short windows — 38/62
// captures the same mean-reversion intent while firing often enough for scalping.
func ScalpingSignal(closes []float64) Signal {
	rsi := RSI(closes, 7)
	if rsi < 38 {
		return Long
	}
	if rsi > 62 {
		return Short
	}
	return None
}

// SwingSignal uses Bollinger Bands(20, 2σ):
// buy when price is at or below lower band, sell at upper band.
func SwingSignal(closes []float64) Signal {
	if len(closes) < 20 {
		return None
	}
	price := closes[len(closes)-1]
	_, upper, lower := BollingerBands(closes, 20, 2.0)
	if upper == 0 {
		return None
	}
	if price <= lower {
		return Long
	}
	if price >= upper {
		return Short
	}
	return None
}

// TrendSignal uses EMA(9) cross EMA(21):
// Long on golden cross, Short on death cross.
func TrendSignal(closes []float64) Signal {
	if len(closes) < 22 {
		return None
	}
	fast := EMA(closes, 9)
	slow := EMA(closes, 21)
	prevFast := EMA(closes[:len(closes)-1], 9)
	prevSlow := EMA(closes[:len(closes)-1], 21)

	if prevFast == 0 || prevSlow == 0 {
		return None
	}
	// Golden cross: fast crosses above slow
	if prevFast <= prevSlow && fast > slow {
		return Long
	}
	// Death cross: fast crosses below slow
	if prevFast >= prevSlow && fast < slow {
		return Short
	}
	return None
}

// MeanReversionSignal uses RSI(14) as primary signal with Bollinger Bands(20,
// 1.5σ) as additional context. Either RSI reaching an extreme OR price touching
// a band is sufficient — the double AND was too strict for forex intraday data
// where both rarely align in a short window.
//
//	LONG  : RSI(14) < 35  OR  price ≤ lower BB(1.5σ)
//	SHORT : RSI(14) > 65  OR  price ≥ upper BB(1.5σ)
//
// Priority: if one fires the other way, RSI wins (stronger signal).
func MeanReversionSignal(closes []float64) Signal {
	if len(closes) < 20 {
		return None
	}
	rsi := RSI(closes, 14)
	price := closes[len(closes)-1]
	_, upper, lower := BollingerBands(closes, 20, 1.5)
	if upper == 0 {
		return None
	}

	rsiLong := rsi < 35
	rsiShort := rsi > 65
	bbLong := price <= lower
	bbShort := price >= upper

	// RSI takes priority; fall back to BB if RSI is neutral
	if rsiLong {
		return Long
	}
	if rsiShort {
		return Short
	}
	if bbLong {
		return Long
	}
	if bbShort {
		return Short
	}
	return None
}
