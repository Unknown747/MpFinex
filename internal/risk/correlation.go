// Package risk provides position-level risk controls beyond per-bot limits,
// including correlation-aware sizing and portfolio-wide exposure caps.
package risk

import (
	"math"
	"sync"
)

// correlationMatrix stores the 15-minute Pearson correlation coefficients for
// the 8 supported forex pairs. Both directions are stored for O(1) lookup.
// Positive value → pairs move together; negative → move opposite.
var correlationMatrix = map[[2]string]float64{
	{"EURUSD", "GBPUSD"}: +0.85,
	{"GBPUSD", "EURUSD"}: +0.85,
	{"EURUSD", "USDCHF"}: -0.92,
	{"USDCHF", "EURUSD"}: -0.92,
	{"USDJPY", "EURJPY"}: +0.78,
	{"EURJPY", "USDJPY"}: +0.78,
	{"EURUSD", "EURJPY"}: +0.72,
	{"EURJPY", "EURUSD"}: +0.72,
	{"GBPUSD", "EURGBP"}: -0.75,
	{"EURGBP", "GBPUSD"}: -0.75,
	{"USDJPY", "USDCHF"}: +0.70,
	{"USDCHF", "USDJPY"}: +0.70,
	{"AUDUSD", "EURUSD"}: +0.65,
	{"EURUSD", "AUDUSD"}: +0.65,
}

// highCorrThreshold is the minimum |correlation| for a pair to be considered
// "highly correlated" and subject to sizing / conflict rules.
const highCorrThreshold = 0.70

// maxExposurePct is the maximum allowed combined risk exposure (% of equity)
// across all open positions that are highly correlated with one another.
const maxExposurePct = 5.0

// riskReductionFactor is applied to the new trade's risk when it enters a
// highly correlated pair that is already in the portfolio in the same direction.
const riskReductionFactor = 0.70 // 30 % reduction

// GetCorrelation returns the Pearson correlation coefficient between two symbols.
// Returns 1.0 for identical symbols and 0.0 when no data is available.
func GetCorrelation(sym1, sym2 string) float64 {
	if sym1 == sym2 {
		return 1.0
	}
	if v, ok := correlationMatrix[[2]string{sym1, sym2}]; ok {
		return v
	}
	return 0.0
}

// OpenPosition represents a live position that is already in the portfolio.
type OpenPosition struct {
	Symbol    string
	Direction string  // "BUY" or "SELL"
	RiskPct   float64 // risk allocated to this position as % of equity
}

// CorrelationManager enforces portfolio-level correlation rules:
//  1. Identical-direction entries into a high-corr pair → 30 % risk cut.
//  2. Opposite-direction entries into a high-corr pair → blocked outright.
//  3. Total correlated exposure cap of 5 % of equity.
//
// All public methods are safe for concurrent use.
type CorrelationManager struct {
	mu sync.RWMutex
}

// NewCorrelationManager creates a ready-to-use CorrelationManager.
func NewCorrelationManager() *CorrelationManager {
	return &CorrelationManager{}
}

// CheckEntry evaluates a proposed new trade against the current open portfolio.
//
// Parameters:
//   - newSymbol:       currency pair to trade
//   - newDirection:    "BUY" or "SELL"
//   - openPositions:   all currently open trades across all bots
//   - currentRiskPct:  the proposed base risk % for the new trade
//
// Returns:
//   - allowed:   false → the trade must not be opened
//   - riskMult:  multiply the base risk by this factor (0.70 or 1.0)
func (cm *CorrelationManager) CheckEntry(
	newSymbol, newDirection string,
	openPositions []OpenPosition,
	currentRiskPct float64,
) (allowed bool, riskMult float64) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	riskMult = 1.0
	totalExposure := currentRiskPct

	for _, pos := range openPositions {
		corr := GetCorrelation(newSymbol, pos.Symbol)
		absCorr := math.Abs(corr)

		if absCorr < highCorrThreshold {
			continue
		}

		// Accumulate exposure only from highly-correlated pairs.
		totalExposure += pos.RiskPct

		// Conflict: positive corr + opposite directions, or
		//           negative corr + same direction.
		sameDir := newDirection == pos.Direction
		conflict := (corr > 0 && !sameDir) || (corr < 0 && sameDir)
		if conflict {
			return false, 0
		}

		// Same-direction high correlation → apply 30 % risk reduction.
		if corr >= highCorrThreshold {
			riskMult = riskReductionFactor
		}
	}

	// Portfolio-wide exposure cap.
	if totalExposure > maxExposurePct {
		return false, 0
	}

	return true, riskMult
}

// TotalCorrelatedExposure returns the sum of risk % across all open positions
// that are highly correlated with each other.
// Used to display current exposure in the metrics dashboard.
func TotalCorrelatedExposure(openPositions []OpenPosition) float64 {
	total := 0.0
	for i, a := range openPositions {
		for j, b := range openPositions {
			if i >= j {
				continue
			}
			if math.Abs(GetCorrelation(a.Symbol, b.Symbol)) >= highCorrThreshold {
				total += a.RiskPct + b.RiskPct
				break
			}
		}
	}
	return total
}

// CorrelationLabel returns a human-readable label for the correlation value.
func CorrelationLabel(corr float64) string {
	abs := math.Abs(corr)
	switch {
	case abs >= 0.90:
		if corr > 0 {
			return "Very High +"
		}
		return "Very High −"
	case abs >= 0.70:
		if corr > 0 {
			return "High +"
		}
		return "High −"
	case abs >= 0.50:
		return "Moderate"
	default:
		return "Low"
	}
}
