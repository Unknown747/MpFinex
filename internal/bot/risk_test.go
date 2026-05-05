package bot

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// ─── CalculateLotSize ─────────────────────────────────────────────────────────

func TestCalculateLotSize_Basic(t *testing.T) {
	info := DefaultSymbolInfo["EURUSD"]
	// equity=$10000, risk=1%, SL=20 pips, pip_value=$10 → riskAmt=$100 → lot=100/(20×10)=0.50
	got := CalculateLotSize(10000, 1.0, 20, info)
	if !approx(got, 0.50, 0.01) {
		t.Errorf("CalculateLotSize basic: want 0.50, got %.2f", got)
	}
}

func TestCalculateLotSize_BelowMinLot(t *testing.T) {
	info := DefaultSymbolInfo["EURUSD"]
	// Very small equity → lot below MinLot → should return MinLot (0.01)
	got := CalculateLotSize(1.0, 0.01, 500, info)
	if !approx(got, info.MinLot, 1e-9) {
		t.Errorf("CalculateLotSize min lot: want %.2f, got %.2f", info.MinLot, got)
	}
}

func TestCalculateLotSize_ZeroSL_ReturnsMinLot(t *testing.T) {
	info := DefaultSymbolInfo["EURUSD"]
	got := CalculateLotSize(10000, 1.5, 0, info)
	if !approx(got, info.MinLot, 1e-9) {
		t.Errorf("CalculateLotSize zero SL: want MinLot %.2f, got %.2f", info.MinLot, got)
	}
}

func TestCalculateLotSize_RoundsToLotStep(t *testing.T) {
	info := DefaultSymbolInfo["EURUSD"] // LotStep = 0.01
	// equity=$10000, risk=1.5%, SL=15 pips → riskAmt=$150 → lot=150/150=1.0 (exact multiple of 0.01)
	got := CalculateLotSize(10000, 1.5, 15, info)
	remainder := math.Mod(math.Round(got/info.LotStep)*info.LotStep-got, info.LotStep)
	if math.Abs(remainder) > 1e-9 {
		t.Errorf("CalculateLotSize: result %.4f is not a multiple of LotStep %.4f", got, info.LotStep)
	}
}

// ─── RiskLimits ───────────────────────────────────────────────────────────────

func makeDummyBot() *Bot {
	return &Bot{
		Name:      "test",
		IsRunning: true,
	}
}

func TestRiskLimits_NoLoss_ReturnsTrue(t *testing.T) {
	rl := NewRiskLimits(2.0, 10.0, 10000)
	b := makeDummyBot()
	ok := rl.CheckDailyLoss(10000, b, 1.0)
	if !ok {
		t.Error("CheckDailyLoss: expected true when equity unchanged")
	}
}

func TestRiskLimits_ExceedsDailyLoss_ReturnsFalse(t *testing.T) {
	rl := NewRiskLimits(2.0, 10.0, 10000) // max daily loss = 2% of $10000 = $200
	b := makeDummyBot()
	ok := rl.CheckDailyLoss(9700, b, 1.0) // $300 loss > $200 limit
	if ok {
		t.Error("CheckDailyLoss: expected false when daily loss exceeded")
	}
	if b.IsRunning {
		t.Error("CheckDailyLoss: bot should be stopped after daily loss exceeded")
	}
}

func TestRiskLimits_DrawdownOK_ReturnsTrue(t *testing.T) {
	rl := NewRiskLimits(2.0, 10.0, 10000)
	b := makeDummyBot()
	// 5% drawdown < 10% limit → should pass
	ok := rl.CheckDrawdown(9500, b, 1.0)
	if !ok {
		t.Error("CheckDrawdown: expected true when drawdown within limit")
	}
}

func TestRiskLimits_ExceedsDrawdown_ReturnsFalse(t *testing.T) {
	rl := NewRiskLimits(2.0, 10.0, 10000) // max drawdown = 10%
	b := makeDummyBot()
	rl.PeakEquity = 10000
	ok := rl.CheckDrawdown(8900, b, 1.0) // 11% drawdown > 10% limit
	if ok {
		t.Error("CheckDrawdown: expected false when drawdown exceeded")
	}
	if b.IsRunning {
		t.Error("CheckDrawdown: bot should be stopped after drawdown exceeded")
	}
}

func TestRiskLimits_PeakEquityUpdates(t *testing.T) {
	rl := NewRiskLimits(5.0, 20.0, 10000)
	b := makeDummyBot()
	rl.CheckDrawdown(12000, b, 1.0) // new peak
	if !approx(rl.PeakEquity, 12000, 1e-9) {
		t.Errorf("PeakEquity should update to 12000, got %.2f", rl.PeakEquity)
	}
}

// ─── RiskProfile ──────────────────────────────────────────────────────────────

func TestRiskProfile_DefaultRisk(t *testing.T) {
	rp := NewRiskProfile()
	if !approx(rp.EffectiveRisk(), 1.5, 1e-9) {
		t.Errorf("Default risk: want 1.5%%, got %.4f%%", rp.EffectiveRisk())
	}
}

func TestRiskProfile_HighWinRate_IncreasesRisk(t *testing.T) {
	rp := NewRiskProfile()
	// 14 wins out of 20 = 70% → riskHigh = 2.0%
	for i := 0; i < 14; i++ {
		rp.RecordResult(true)
	}
	for i := 0; i < 6; i++ {
		rp.RecordResult(false)
	}
	if !approx(rp.CurrentRiskPercent, 2.0, 1e-9) {
		t.Errorf("HighWinRate: want 2.0%%, got %.4f%%", rp.CurrentRiskPercent)
	}
}

func TestRiskProfile_LowWinRate_DecreasesRisk(t *testing.T) {
	rp := NewRiskProfile()
	// 6 wins out of 20 = 30% → riskLow = 1.0%
	for i := 0; i < 6; i++ {
		rp.RecordResult(true)
	}
	for i := 0; i < 14; i++ {
		rp.RecordResult(false)
	}
	if !approx(rp.CurrentRiskPercent, 1.0, 1e-9) {
		t.Errorf("LowWinRate: want 1.0%%, got %.4f%%", rp.CurrentRiskPercent)
	}
}

func TestRiskProfile_ConsecutiveLoss3_HalvesRisk(t *testing.T) {
	rp := NewRiskProfile()
	rp.RecordResult(false)
	rp.RecordResult(false)
	rp.RecordResult(false) // 3 consecutive losses
	effective := rp.EffectiveRisk()
	base := rp.CurrentRiskPercent
	if !approx(effective, base*0.5, 1e-9) {
		t.Errorf("3 consecutive losses: want %.4f%%, got %.4f%%", base*0.5, effective)
	}
}

func TestRiskProfile_ConsecutiveLoss5_Cooldown(t *testing.T) {
	rp := NewRiskProfile()
	for i := 0; i < 5; i++ {
		rp.RecordResult(false)
	}
	if !rp.IsCoolingDown() {
		t.Error("After 5 consecutive losses, bot should be cooling down")
	}
}

func TestRiskProfile_WinResetsCooldown(t *testing.T) {
	rp := NewRiskProfile()
	rp.RecordResult(false)
	rp.RecordResult(false)
	rp.RecordResult(false)
	if rp.ConsecutiveLoss != 3 {
		t.Fatalf("Expected ConsecutiveLoss=3, got %d", rp.ConsecutiveLoss)
	}
	rp.RecordResult(true) // win resets counter
	if rp.ConsecutiveLoss != 0 {
		t.Errorf("Win should reset ConsecutiveLoss to 0, got %d", rp.ConsecutiveLoss)
	}
}

func TestRiskProfile_RollingWinRate(t *testing.T) {
	rp := NewRiskProfile()
	// 10 wins, 10 losses → 50%
	for i := 0; i < 10; i++ {
		rp.RecordResult(true)
		rp.RecordResult(false)
	}
	got := rp.RollingWinRate()
	if !approx(got, 50.0, 1e-9) {
		t.Errorf("RollingWinRate: want 50.0, got %.2f", got)
	}
}

func TestRiskProfile_RollingWindowCapped(t *testing.T) {
	rp := NewRiskProfile()
	// Record 25 results (> riskRollingWindow=20): last 20 only
	// 5 old wins (will be evicted), then 15 losses, then 5 wins = last 20 = 5W+15L → 25%
	for i := 0; i < 5; i++ {
		rp.RecordResult(true)
	}
	for i := 0; i < 15; i++ {
		rp.RecordResult(false)
	}
	for i := 0; i < 5; i++ {
		rp.RecordResult(true)
	}
	if len(rp.results) > riskRollingWindow {
		t.Errorf("Rolling window exceeded %d, got %d", riskRollingWindow, len(rp.results))
	}
}
