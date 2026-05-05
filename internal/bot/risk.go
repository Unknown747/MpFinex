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
}

// DefaultSymbolInfo menyimpan spesifikasi default untuk semua pasangan yang didukung.
var DefaultSymbolInfo = map[string]SymbolInfo{
        "EURUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "GBPUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "USDJPY": {PipSize: 0.01, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "AUDUSD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "USDCAD": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "USDCHF": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "EURGBP": {PipSize: 0.0001, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
        "EURJPY": {PipSize: 0.01, PipValue: 10.0, MinLot: 0.01, LotStep: 0.01, ContractSize: 100000},
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
