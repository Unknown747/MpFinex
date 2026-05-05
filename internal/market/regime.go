// Package market — regime detection.
// Detects whether a symbol is in a Trending, Ranging, or Volatile market
// state and provides per-strategy weight / risk multipliers.
// Results are cached with a 1-hour TTL to avoid redundant computation.
package market

import (
	"math"
	"sync"
	"time"

	"github.com/finex/finex-cli/internal/indicator"
)

// Regime represents the detected market condition for a symbol.
type Regime int

const (
	RegimeUnknown  Regime = iota
	RegimeTrending        // ADX > 25 + meaningful EMA slope
	RegimeRanging         // ADX < 20 + narrow Bollinger Band width
	RegimeVolatile        // ATR(14) > 1.5× mean ATR over 50 periods
)

// String returns the display name for the regime.
func (r Regime) String() string {
	switch r {
	case RegimeTrending:
		return "TRENDING"
	case RegimeRanging:
		return "RANGING"
	case RegimeVolatile:
		return "VOLATILE"
	default:
		return "UNKNOWN"
	}
}

// regimeEntry is a single cached regime result.
type regimeEntry struct {
	regime    Regime
	adx       float64
	atr       float64
	updatedAt time.Time
}

// RegimeDetector detects and caches market regimes per symbol.
// All public methods are safe for concurrent use.
type RegimeDetector struct {
	mu    sync.RWMutex
	cache map[string]regimeEntry
	ttl   time.Duration
}

// NewRegimeDetector creates a RegimeDetector with a 1-hour TTL.
func NewRegimeDetector() *RegimeDetector {
	return &RegimeDetector{
		cache: make(map[string]regimeEntry),
		ttl:   time.Hour,
	}
}

// Detect returns the current market regime for the symbol.
// Uses the cached value if it was computed within the last hour;
// otherwise recomputes from the current market data.
func (rd *RegimeDetector) Detect(symbol string, mkt *Market) Regime {
	rd.mu.RLock()
	if e, ok := rd.cache[symbol]; ok && time.Since(e.updatedAt) < rd.ttl {
		rd.mu.RUnlock()
		return e.regime
	}
	rd.mu.RUnlock()

	return rd.forceUpdate(symbol, mkt)
}

// ForceUpdate recomputes and stores the regime regardless of TTL.
// Called by the hourly ticker in main.go.
func (rd *RegimeDetector) ForceUpdate(symbol string, mkt *Market) Regime {
	return rd.forceUpdate(symbol, mkt)
}

// GetCached returns the last cached regime without triggering recomputation.
// Returns RegimeUnknown if the symbol has not been computed yet.
func (rd *RegimeDetector) GetCached(symbol string) Regime {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	if e, ok := rd.cache[symbol]; ok {
		return e.regime
	}
	return RegimeUnknown
}

// GetCachedADX returns the ADX value from the last detection run.
func (rd *RegimeDetector) GetCachedADX(symbol string) float64 {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.cache[symbol].adx
}

// GetCachedATR returns the ATR(14) value from the last detection run.
func (rd *RegimeDetector) GetCachedATR(symbol string) float64 {
	rd.mu.RLock()
	defer rd.mu.RUnlock()
	return rd.cache[symbol].atr
}

// forceUpdate performs the actual computation and caches the result.
func (rd *RegimeDetector) forceUpdate(symbol string, mkt *Market) Regime {
	closes := mkt.GetCloses(symbol)
	highs, lows := mkt.GetHighLows(symbol)

	regime, adxVal, atrVal := detectRegime(closes, highs, lows)

	rd.mu.Lock()
	rd.cache[symbol] = regimeEntry{
		regime:    regime,
		adx:       adxVal,
		atr:       atrVal,
		updatedAt: time.Now(),
	}
	rd.mu.Unlock()

	return regime
}

// detectRegime performs the actual ADX / ATR / BB analysis.
// Extracted for testability.
func detectRegime(closes, highs, lows []float64) (Regime, float64, float64) {
	if len(closes) < 51 || len(highs) < 51 || len(lows) < 51 {
		return RegimeUnknown, 0, 0
	}

	atr14 := indicator.ATR(highs, lows, closes, 14)
	meanATR := computeMeanATR(highs, lows, closes, 14, 50)

	// ── Volatile: current ATR is 50 % above the 50-period mean ─────────────
	if meanATR > 0 && atr14 > 1.5*meanATR {
		return RegimeVolatile, 0, atr14
	}

	adx := indicator.ADX(highs, lows, closes, 14)

	// ── Trending: ADX > 25 + EMA slope not flat ──────────────────────────
	if adx > 25 {
		ema9 := indicator.EMA(closes, 9)
		var ema9prev float64
		if len(closes) > 1 {
			ema9prev = indicator.EMA(closes[:len(closes)-1], 9)
		}
		emaSlope := ema9 - ema9prev
		if math.Abs(emaSlope) > 0 {
			return RegimeTrending, adx, atr14
		}
	}

	// ── Ranging: ADX < 20 + narrow Bollinger Bands ───────────────────────
	if adx < 20 {
		bbMid, bbUpper, bbLower := indicator.BollingerBands(closes, 20, 2.0)
		if bbMid > 0 {
			bbWidth := (bbUpper - bbLower) / bbMid * 100
			if bbWidth < 1.0 {
				return RegimeRanging, adx, atr14
			}
		}
	}

	// ── Default fallback ────────────────────────────────────────────────
	if adx >= 20 {
		return RegimeTrending, adx, atr14
	}
	return RegimeRanging, adx, atr14
}

// computeMeanATR calculates the rolling mean of ATR(period) over `n` windows.
// Each window ends one candle later than the previous.
func computeMeanATR(highs, lows, closes []float64, period, n int) float64 {
	total := len(closes)
	if total < period+n {
		return 0
	}
	sum := 0.0
	count := 0
	for i := total - n; i < total; i++ {
		end := i + 1
		start := end - period - 1
		if start < 0 {
			start = 0
		}
		atr := indicator.ATR(highs[start:end], lows[start:end], closes[start:end], period)
		if atr > 0 {
			sum += atr
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// ─── Strategy weighting & risk scaling ───────────────────────────────────────

// RiskMultiplier returns the risk scaling factor for a given regime.
//   - Volatile → 0.5 (50 % risk reduction across all strategies)
//   - Trending / Ranging / Unknown → 1.0 (no global reduction)
func RiskMultiplier(r Regime) float64 {
	if r == RegimeVolatile {
		return 0.5
	}
	return 1.0
}

// StrategyWeight returns the weight (0.0–1.0) for a strategy in a regime.
// Used by the metrics dashboard to highlight optimal strategies.
//
//   - Trending  → Trend Following + Swing Trading favoured (0.70)
//   - Ranging   → Mean Reversion + Scalping favoured (0.70)
//   - Volatile  → all strategies at 0.50
//   - Unknown   → all strategies at 1.0 (no filter)
func StrategyWeight(r Regime, strategy string) float64 {
	switch r {
	case RegimeTrending:
		switch strategy {
		case "Trend Following", "Swing Trading":
			return 0.70
		default:
			return 0.30
		}
	case RegimeRanging:
		switch strategy {
		case "Mean Reversion", "Scalping":
			return 0.70
		default:
			return 0.30
		}
	case RegimeVolatile:
		return 0.50
	default:
		return 1.0
	}
}
