package risk

import (
	"testing"
)

func TestGetCorrelation(t *testing.T) {
	tests := []struct {
		sym1, sym2 string
		wantMin    float64
		wantMax    float64
	}{
		{"EURUSD", "GBPUSD", 0.80, 0.90},
		{"EURUSD", "USDCHF", -0.95, -0.88},
		{"USDJPY", "EURJPY", 0.70, 0.85},
		{"EURUSD", "EURUSD", 1.0, 1.0},
		{"EURUSD", "AUDUSD", 0.60, 0.70},
		{"USDCAD", "GBPUSD", -0.01, 0.01}, // no data → 0
	}
	for _, tc := range tests {
		corr := GetCorrelation(tc.sym1, tc.sym2)
		if corr < tc.wantMin || corr > tc.wantMax {
			t.Errorf("GetCorrelation(%s,%s) = %.2f, want [%.2f, %.2f]",
				tc.sym1, tc.sym2, corr, tc.wantMin, tc.wantMax)
		}
	}
}

func TestGetCorrelationSymmetric(t *testing.T) {
	pairs := [][2]string{
		{"EURUSD", "GBPUSD"},
		{"EURUSD", "USDCHF"},
		{"USDJPY", "EURJPY"},
	}
	for _, p := range pairs {
		a := GetCorrelation(p[0], p[1])
		b := GetCorrelation(p[1], p[0])
		if a != b {
			t.Errorf("correlation not symmetric: %s/%s = %.2f, %s/%s = %.2f",
				p[0], p[1], a, p[1], p[0], b)
		}
	}
}

func TestCheckEntry_NoConflict(t *testing.T) {
	cm := NewCorrelationManager()
	open := []OpenPosition{
		{Symbol: "USDJPY", Direction: "BUY", RiskPct: 1.5},
	}
	allowed, mult := cm.CheckEntry("EURUSD", "BUY", open, 1.5)
	if !allowed {
		t.Error("expected allowed=true for uncorrelated pairs")
	}
	if mult != 1.0 {
		t.Errorf("expected riskMult=1.0, got %.2f", mult)
	}
}

func TestCheckEntry_SameDirHighCorr_ReducesRisk(t *testing.T) {
	cm := NewCorrelationManager()
	open := []OpenPosition{
		{Symbol: "GBPUSD", Direction: "BUY", RiskPct: 1.5},
	}
	// EURUSD/GBPUSD corr = +0.85: same direction → 30% reduction
	allowed, mult := cm.CheckEntry("EURUSD", "BUY", open, 1.5)
	if !allowed {
		t.Error("expected allowed=true for same-direction high-corr pair")
	}
	if mult != riskReductionFactor {
		t.Errorf("expected riskMult=%.2f, got %.2f", riskReductionFactor, mult)
	}
}

func TestCheckEntry_OppositeDirHighCorr_Blocked(t *testing.T) {
	cm := NewCorrelationManager()
	open := []OpenPosition{
		{Symbol: "GBPUSD", Direction: "BUY", RiskPct: 1.5},
	}
	// EURUSD/GBPUSD corr = +0.85: opposite direction → blocked
	allowed, mult := cm.CheckEntry("EURUSD", "SELL", open, 1.5)
	if allowed {
		t.Error("expected allowed=false for opposite-direction high-corr pair")
	}
	if mult != 0 {
		t.Errorf("expected riskMult=0, got %.2f", mult)
	}
}

func TestCheckEntry_NegativeCorr_SameDir_Blocked(t *testing.T) {
	cm := NewCorrelationManager()
	open := []OpenPosition{
		{Symbol: "USDCHF", Direction: "BUY", RiskPct: 1.5},
	}
	// EURUSD/USDCHF corr = -0.92: same direction with inverse pair → conflict
	allowed, _ := cm.CheckEntry("EURUSD", "BUY", open, 1.5)
	if allowed {
		t.Error("expected allowed=false for same-direction inverse-corr pair")
	}
}

func TestCheckEntry_NegativeCorr_OppositeDir_Allowed(t *testing.T) {
	cm := NewCorrelationManager()
	open := []OpenPosition{
		{Symbol: "USDCHF", Direction: "BUY", RiskPct: 1.5},
	}
	// EURUSD/USDCHF corr = -0.92: opposite direction is fine (they move together effectively)
	allowed, _ := cm.CheckEntry("EURUSD", "SELL", open, 1.5)
	if !allowed {
		t.Error("expected allowed=true for opposite-direction inverse-corr pair")
	}
}

func TestCheckEntry_ExposureCap(t *testing.T) {
	cm := NewCorrelationManager()
	// Fill up to near the 5% cap with correlated positions
	open := []OpenPosition{
		{Symbol: "GBPUSD", Direction: "BUY", RiskPct: 2.0},
		{Symbol: "EURJPY", Direction: "BUY", RiskPct: 2.0},
	}
	// New 2% trade pushes total correlated exposure way above 5%
	allowed, _ := cm.CheckEntry("EURUSD", "BUY", open, 2.0)
	if allowed {
		t.Error("expected allowed=false when total exposure exceeds 5%")
	}
}

func TestTotalCorrelatedExposure_Empty(t *testing.T) {
	exp := TotalCorrelatedExposure(nil)
	if exp != 0 {
		t.Errorf("expected 0 for empty positions, got %.2f", exp)
	}
}

func TestCorrelationLabel(t *testing.T) {
	tests := []struct {
		corr float64
		want string
	}{
		{+0.92, "Very High +"},
		{-0.92, "Very High −"},
		{+0.75, "High +"},
		{-0.75, "High −"},
		{+0.55, "Moderate"},
		{+0.10, "Low"},
	}
	for _, tc := range tests {
		got := CorrelationLabel(tc.corr)
		if got != tc.want {
			t.Errorf("CorrelationLabel(%.2f) = %q, want %q", tc.corr, got, tc.want)
		}
	}
}
