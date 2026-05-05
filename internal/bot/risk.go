package bot

import "time"

// RiskLimits melindungi akun dari kerugian berlebihan via daily loss limit
// dan max drawdown trailing. Attach ke Bot.Risk untuk mengaktifkan.
type RiskLimits struct {
	MaxDailyLossPercent float64
	MaxDrawdownPercent  float64
	InitialEquity       float64
	PeakEquity          float64
	DailyLoss           float64
	LastReset           time.Time
}

// NewRiskLimits membuat RiskLimits baru dengan equity awal sebagai baseline.
func NewRiskLimits(maxDailyLossPct, maxDrawdownPct, initialEquity float64) *RiskLimits {
	return &RiskLimits{
		MaxDailyLossPercent: maxDailyLossPct,
		MaxDrawdownPercent:  maxDrawdownPct,
		InitialEquity:       initialEquity,
		PeakEquity:          initialEquity,
		LastReset:           time.Now(),
	}
}

// CheckDailyLoss memeriksa apakah kerugian hari ini sudah melebihi batas.
// Reset otomatis setiap hari baru. Jika limit terlewati, tutup semua posisi
// dan hentikan bot, lalu return false.
func (r *RiskLimits) CheckDailyLoss(currentEquity float64, b *Bot, currentPrice float64) bool {
	now := time.Now()
	if now.YearDay() != r.LastReset.YearDay() || now.Year() != r.LastReset.Year() {
		r.DailyLoss = 0
		r.InitialEquity = currentEquity
		r.LastReset = now
	}

	r.DailyLoss = r.InitialEquity - currentEquity
	limit := r.MaxDailyLossPercent / 100 * r.InitialEquity
	if r.DailyLoss > limit {
		b.CloseAllPositions(currentPrice)
		b.IsRunning = false
		return false
	}
	return true
}

// CheckDrawdown memeriksa drawdown dari peak equity tertinggi.
// Jika drawdown melebihi MaxDrawdownPercent, tutup semua posisi dan hentikan bot.
func (r *RiskLimits) CheckDrawdown(currentEquity float64, b *Bot, currentPrice float64) bool {
	if currentEquity > r.PeakEquity {
		r.PeakEquity = currentEquity
	}

	if r.PeakEquity == 0 {
		return true
	}
	drawdown := (r.PeakEquity - currentEquity) / r.PeakEquity * 100
	if drawdown > r.MaxDrawdownPercent {
		b.CloseAllPositions(currentPrice)
		b.IsRunning = false
		return false
	}
	return true
}
