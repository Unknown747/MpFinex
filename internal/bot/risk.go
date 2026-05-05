package bot

import (
	"math"
	"time"
)

// ─── Symbol Info ──────────────────────────────────────────────────────────────

// SymbolInfo menyimpan spesifikasi kontrak untuk setiap pasangan forex.
type SymbolInfo struct {
	PipSize      float64 // ukuran 1 pip dalam harga (e.g. 0.0001 untuk EURUSD)
	PipValue     float64 // nilai USD per pip per 1 lot standard (biasanya $10)
	MinLot       float64 // ukuran lot minimum (e.g. 0.01)
	LotStep      float64 // kelipatan lot (e.g. 0.01)
	ContractSize float64 // unit per lot standard (100,000 untuk forex major)
	Spread       float64 // spread tipikal dalam unit harga (e.g. 0.0002 = 2 pip untuk EURUSD)
}

// DefaultSymbolInfo menyimpan spesifikasi default untuk semua pasangan yang didukung.
var DefaultSymbolInfo = map[string]SymbolInfo{
	"EURUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0002},
	"GBPUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0003},
	"USDJPY": {PipSize: 0.01, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.020},
	"AUDUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0003},
	"USDCAD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0003},
	"USDCHF": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0003},
	"EURGBP": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.0003},
	"EURJPY": {PipSize: 0.01, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000, Spread: 0.030},
}

// CalculateLotSize menghitung ukuran lot berdasarkan risiko per trade.
//
// Rumus: lot_size = (equity × riskPercent/100) / (stopLossPips × pip_value)
//
// Hasil dibulatkan ke lot step terdekat dan tidak kurang dari MinLot.
// Default risk percent: 1.5% per trade.
func CalculateLotSize(equity, riskPercent, stopLossPips float64, info SymbolInfo) float64 {
	if stopLossPips <= 0 || info.PipValue <= 0 {
		return info.MinLot
	}
	riskAmount := equity * riskPercent / 100
	lotSize := riskAmount / (stopLossPips * info.PipValue)

	// Bulatkan ke lot step terdekat
	if info.LotStep > 0 {
		lotSize = math.Round(lotSize/info.LotStep) * info.LotStep
	}
	if lotSize < info.MinLot {
		lotSize = info.MinLot
	}
	return lotSize
}

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

// ─── RiskProfile — Dynamic Risk Adjustment ────────────────────────────────────

// RiskProfile melacak performa rolling 20 trade terakhir dan menyesuaikan
// risk percent secara dinamis berdasarkan win rate dan consecutive losses.
//
// Aturan penyesuaian:
//   - Win rate > 60% → risk = 2.0% per trade
//   - Win rate 40–60% → risk = 1.5% per trade (default)
//   - Win rate < 40% → risk = 1.0% per trade
//   - Consecutive loss = 3 → potong risk 50% untuk trade berikutnya
//   - Consecutive loss = 5 → cooldown 1 jam (stop trading)
//   - Win trade → reset consecutive loss ke 0
type RiskProfile struct {
	results            []bool    // rolling window hasil trade (true = win)
	ConsecutiveLoss    int       // jumlah loss berturut-turut saat ini
	CurrentRiskPercent float64   // risk pct berdasarkan win rate (sebelum adj consecutive)
	coolingUntil       time.Time // waktu selesai cooldown; zero = tidak cooldown
}

const (
	riskRollingWindow = 20
	riskHigh          = 2.0 // win rate > 60%
	riskMid           = 1.5 // win rate 40–60%
	riskLow           = 1.0 // win rate < 40%
	cooldownDuration  = time.Hour
)

// NewRiskProfile membuat RiskProfile baru dengan risk default 1.5%.
func NewRiskProfile() *RiskProfile {
	return &RiskProfile{
		results:            make([]bool, 0, riskRollingWindow),
		CurrentRiskPercent: riskMid,
	}
}

// RecordResult mencatat hasil trade dan memperbarui profil risiko.
// Dipanggil otomatis oleh Bot.closeTrade() setiap kali trade ditutup.
func (rp *RiskProfile) RecordResult(win bool) {
	rp.results = append(rp.results, win)
	if len(rp.results) > riskRollingWindow {
		rp.results = rp.results[len(rp.results)-riskRollingWindow:]
	}

	if win {
		rp.ConsecutiveLoss = 0
	} else {
		rp.ConsecutiveLoss++
	}

	// Cooldown 1 jam setelah 5 loss berturut-turut
	if rp.ConsecutiveLoss >= 5 {
		rp.coolingUntil = time.Now().Add(cooldownDuration)
	}

	// Perbarui CurrentRiskPercent berdasarkan rolling win rate
	wins := 0
	for _, r := range rp.results {
		if r {
			wins++
		}
	}
	total := len(rp.results)
	if total == 0 {
		rp.CurrentRiskPercent = riskMid
		return
	}
	wr := float64(wins) / float64(total) * 100
	switch {
	case wr > 60:
		rp.CurrentRiskPercent = riskHigh
	case wr >= 40:
		rp.CurrentRiskPercent = riskMid
	default:
		rp.CurrentRiskPercent = riskLow
	}
}

// EffectiveRisk mengembalikan risk percent efektif untuk trade berikutnya.
// Jika consecutive loss ≥ 3, risk dipotong 50% (minimum 0.1%).
func (rp *RiskProfile) EffectiveRisk() float64 {
	r := rp.CurrentRiskPercent
	if rp.ConsecutiveLoss >= 3 {
		r *= 0.5
	}
	if r < 0.1 {
		r = 0.1
	}
	return r
}

// IsCoolingDown mengembalikan true jika bot sedang dalam periode cooldown
// (dipicu oleh 5 consecutive loss → trading halt 1 jam).
func (rp *RiskProfile) IsCoolingDown() bool {
	return time.Now().Before(rp.coolingUntil)
}

// RollingWinRate mengembalikan win rate rolling window sebagai persentase (0–100).
func (rp *RiskProfile) RollingWinRate() float64 {
	if len(rp.results) == 0 {
		return 0
	}
	wins := 0
	for _, r := range rp.results {
		if r {
			wins++
		}
	}
	return float64(wins) / float64(len(rp.results)) * 100
}
