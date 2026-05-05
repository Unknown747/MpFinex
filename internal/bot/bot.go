package bot

import (
        "fmt"
        "math/rand"
        "time"

        "github.com/finex/finex-cli/internal/indicator"
        "github.com/finex/finex-cli/internal/market"
        "github.com/finex/finex-cli/internal/strategy"
        "github.com/finex/finex-cli/internal/utils"
)

type Strategy string

const (
        Scalping       Strategy = "Scalping"
        SwingTrading   Strategy = "Swing Trading"
        TrendFollowing Strategy = "Trend Following"
        MeanReversion  Strategy = "Mean Reversion"
)

var AllStrategies = []Strategy{
        Scalping, SwingTrading, TrendFollowing, MeanReversion,
}

type TradeSide string

const (
        Buy  TradeSide = "BUY"
        Sell TradeSide = "SELL"
)

type TradeStatus string

const (
        Open   TradeStatus = "OPEN"
        Closed TradeStatus = "CLOSED"
)

type Trade struct {
        ID         int
        BotID      int
        Symbol     string
        Side       TradeSide
        Quantity   float64
        EntryPrice float64
        ExitPrice  float64
        PnL        float64
        Status     TradeStatus
        OpenedAt   time.Time
        ClosedAt   time.Time

        // ATR-based absolute SL/TP (0 = gunakan % berbasis TakeProfitPct/StopLossPct)
        SLPrice float64
        TPPrice float64

        // Trailing stop state
        BreakevenSet   bool
        TrailingActive bool

        // true jika order ini adalah paper trade (dry-run mode)
        IsDryRun bool

        // Jumlah tick sejak trade dibuka — digunakan oleh MonitorDivergence (scan tiap 10 tick).
        TicksSinceOpen int
}

// TradeEvent carries all info about a trade open or close.
type TradeEvent struct {
        Bot   *Bot
        Trade *Trade
}

type Bot struct {
        ID            int
        Name          string
        Symbol        string
        Strategy      Strategy
        IsRunning     bool
        RiskPct       float64
        TakeProfitPct float64
        StopLossPct   float64
        TotalPnL      float64
        WinCount      int
        LossCount     int
        Trades        []*Trade
        OpenTrade     *Trade
        rng           *rand.Rand
        tradeCounter  int

        // Risk management — nil means no limits enforced.
        Risk *RiskLimits

        // DryRun true → order hanya disimulasikan (paper trade), tidak dikirim ke MT5.
        // Slippage acak ±1 pip diterapkan untuk realisme.
        DryRun bool

        // Optional callbacks fired on trade events (set by main to wire logger).
        OnTradeOpen  func(ev TradeEvent)
        OnTradeClose func(ev TradeEvent)
}

func NewBot(id int, name, symbol string, strategy Strategy, risk, tp, sl float64) *Bot {
        return &Bot{
                ID:            id,
                Name:          name,
                Symbol:        symbol,
                Strategy:      strategy,
                IsRunning:     false,
                RiskPct:       risk,
                TakeProfitPct: tp,
                StopLossPct:   sl,
                Trades:        make([]*Trade, 0),
                rng:           rand.New(rand.NewSource(time.Now().UnixNano() + int64(id))),
        }
}

func (b *Bot) Toggle() {
        b.IsRunning = !b.IsRunning
}

func (b *Bot) WinRate() float64 {
        total := b.WinCount + b.LossCount
        if total == 0 {
                return 0
        }
        return float64(b.WinCount) / float64(total) * 100
}

func (b *Bot) TradeCount() int {
        return len(b.Trades)
}

// CloseAllPositions menutup posisi yang sedang terbuka pada harga saat ini.
// Dipanggil oleh RiskLimits saat daily loss limit atau max drawdown terlewati.
func (b *Bot) CloseAllPositions(currentPrice float64) {
        if b.OpenTrade == nil {
                return
        }
        entry := b.OpenTrade.EntryPrice
        var pnlPct float64
        if b.OpenTrade.Side == Buy {
                pnlPct = (currentPrice - entry) / entry
        } else {
                pnlPct = (entry - currentPrice) / entry
        }
        b.closeTrade(currentPrice, pnlPct)
}

func (b *Bot) Tick(mkt *market.Market, accountBalance float64) {
        if !b.IsRunning {
                return
        }

        price := mkt.GetPrice(b.Symbol)
        if price == nil {
                return
        }

        if b.OpenTrade != nil {
                highs, lows := mkt.GetHighLows(b.Symbol)
                b.checkCloseCondition(price.Price, mkt.GetCloses(b.Symbol), highs, lows)
                return
        }

        // Periksa risk limits sebelum membuka posisi baru
        if b.Risk != nil {
                if !b.Risk.CheckDailyLoss(accountBalance, b, price.Price) {
                        return
                }
                if !b.Risk.CheckDrawdown(accountBalance, b, price.Price) {
                        return
                }
        }

        // News blackout — lewati semua entry saat ada rilis berita berdampak tinggi
        if utils.IsNewsTime() {
                return
        }

        closes := mkt.GetCloses(b.Symbol)
        sig := b.getSignal(closes)
        if sig != indicator.None {
                // Multi-TF confirmation untuk semua strategy kecuali Scalping
                if b.Strategy != Scalping {
                        direction := "BUY"
                        if sig == indicator.Short {
                                direction = "SELL"
                        }
                        if !ConfirmHigherTF(b.Symbol, direction, mkt) {
                                return // Higher TF tidak align, tunda entry
                        }
                }
                b.openTrade(price.Price, accountBalance, sig, mkt)
        }
}

// getSignal evaluates the indicator for this bot's strategy and returns a
// directional signal. None means "no trade yet".
func (b *Bot) getSignal(closes []float64) indicator.Signal {
        switch b.Strategy {
        case Scalping:
                return indicator.ScalpingSignal(closes)
        case SwingTrading:
                return indicator.SwingSignal(closes)
        case TrendFollowing:
                return indicator.TrendSignal(closes)
        case MeanReversion:
                return indicator.MeanReversionSignal(closes)
        }
        return indicator.None
}

func (b *Bot) openTrade(price, balance float64, sig indicator.Signal, mkt *market.Market) {
        side := Buy
        if sig == indicator.Short {
                side = Sell
        }

        // ── Smart Money Confirmation (non-Scalping) ───────────────────────────────
        // Entry hanya diijinkan jika ada Order Block ATAU Fair Value Gap dalam
        // 5 candle terakhir. Scalping dibebaskan karena waktu entry sangat sempit.
        if b.Strategy != Scalping {
                direction := "BUY"
                if side == Sell {
                        direction = "SELL"
                }
                candles := mkt.GetHistory(b.Symbol)
                smResult := strategy.Analyze(candles, direction)
                if !smResult.Confirmed {
                        return // Tidak ada konfirmasi SMC — tunda entry
                }
        }

        // Terapkan slippage acak ±1 pip pada paper trade (dry-run mode)
        entryPrice := price
        if b.DryRun {
                symInfo := DefaultSymbolInfo[b.Symbol]
                pipSize := symInfo.PipSize
                if pipSize == 0 {
                        pipSize = 0.0001
                }
                slippage := (b.rng.Float64()*2 - 1) * pipSize
                entryPrice = price + slippage
        }

        // Hitung ATR-based SL/TP untuk semua strategy kecuali Scalping
        var slPrice, tpPrice, qty float64
        if b.Strategy != Scalping {
                highs, lows := mkt.GetHighLows(b.Symbol)
                closes := mkt.GetCloses(b.Symbol)
                atr := indicator.ATR(highs, lows, closes, 14)

                symInfo := DefaultSymbolInfo[b.Symbol]
                pipSize := symInfo.PipSize
                if pipSize == 0 {
                        pipSize = 0.0001
                }

                if atr > 0 {
                        slDist := 1.5 * atr
                        tpDist := 3.0 * atr
                        if side == Buy {
                                slPrice = entryPrice - slDist
                                tpPrice = entryPrice + tpDist
                        } else {
                                slPrice = entryPrice + slDist
                                tpPrice = entryPrice - tpDist
                        }
                        // Dynamic lot sizing berbasis ATR stop loss
                        stopLossPips := slDist / pipSize
                        qty = CalculateLotSize(balance, b.RiskPct, stopLossPips, symInfo)
                }
        }

        // Fallback ke fixed lot sizing jika ATR tidak tersedia (atau Scalping)
        if qty == 0 {
                risk := balance * (b.RiskPct / 100)
                if entryPrice > 0 {
                        qty = risk / entryPrice
                }
                if qty == 0 {
                        qty = 0.01
                }
        }

        b.tradeCounter++
        trade := &Trade{
                ID:         b.tradeCounter,
                BotID:      b.ID,
                Symbol:     b.Symbol,
                Side:       side,
                Quantity:   qty,
                EntryPrice: entryPrice,
                SLPrice:    slPrice,
                TPPrice:    tpPrice,
                Status:     Open,
                OpenedAt:   time.Now(),
                IsDryRun:   b.DryRun,
        }
        b.OpenTrade = trade

        if b.OnTradeOpen != nil {
                b.OnTradeOpen(TradeEvent{Bot: b, Trade: trade})
        }
}

// checkCloseCondition exits the trade on TP/SL hit.
//
// Priority:
//  1. Update trailing stop / breakeven setiap tick.
//  2. RSI divergence check setiap 10 tick (early exit).
//  3. Gunakan SLPrice/TPPrice absolut jika sudah di-set (ATR-based).
//  4. Fallback ke % berbasis TakeProfitPct/StopLossPct (Scalping + ATR data kurang).
//  5. Secondary exit (TrendFollowing only): potong posisi saat sinyal berbalik.
func (b *Bot) checkCloseCondition(currentPrice float64, closes, highs, lows []float64) {
        if b.OpenTrade == nil {
                return
        }

        // Perbarui breakeven / trailing stop setiap tick (juga increment TicksSinceOpen)
        UpdateTrailingStop(b.OpenTrade, currentPrice, b.Symbol)

        entry := b.OpenTrade.EntryPrice
        pnlPct := pnlPercent(b.OpenTrade.Side, entry, currentPrice)

        // ── RSI Divergence: early exit setiap 10 tick ─────────────────────────────
        divDir := "BUY"
        if b.OpenTrade.Side == Sell {
                divDir = "SELL"
        }
        if MonitorDivergence(highs, lows, closes, divDir, b.OpenTrade.TicksSinceOpen) {
                b.closeTrade(currentPrice, pnlPct)
                return
        }

        // ── SL check: absolut jika SLPrice di-set, kalau tidak pakai % ───────────
        slHit := false
        if b.OpenTrade.SLPrice > 0 {
                if b.OpenTrade.Side == Buy && currentPrice <= b.OpenTrade.SLPrice {
                        slHit = true
                } else if b.OpenTrade.Side == Sell && currentPrice >= b.OpenTrade.SLPrice {
                        slHit = true
                }
        } else {
                slHit = pnlPct <= -(b.StopLossPct / 100)
        }

        // ── TP check: absolut jika TPPrice di-set, kalau tidak pakai % ───────────
        tpHit := false
        if b.OpenTrade.TPPrice > 0 {
                if b.OpenTrade.Side == Buy && currentPrice >= b.OpenTrade.TPPrice {
                        tpHit = true
                } else if b.OpenTrade.Side == Sell && currentPrice <= b.OpenTrade.TPPrice {
                        tpHit = true
                }
        } else {
                tpHit = pnlPct >= (b.TakeProfitPct / 100)
        }

        if tpHit || slHit {
                b.closeTrade(currentPrice, pnlPct)
                return
        }

        // Secondary exit (trend strategies only): cut when momentum reverses.
        // Scalping uses tight TP/SL — no reversal exit needed.
        // Swing & MeanReversion are contrarian — hold through pullbacks.
        if b.Strategy == TrendFollowing {
                sig := b.getSignal(closes)
                if sig != indicator.None {
                        isLong := b.OpenTrade.Side == Buy
                        if (isLong && sig == indicator.Short) || (!isLong && sig == indicator.Long) {
                                b.closeTrade(currentPrice, pnlPct)
                        }
                }
        }
}

func (b *Bot) closeTrade(exitPrice, pnlPct float64) {
        trade := b.OpenTrade
        trade.ExitPrice = exitPrice
        trade.Status = Closed
        trade.ClosedAt = time.Now()

        positionValue := trade.Quantity * trade.EntryPrice
        trade.PnL = positionValue * pnlPct

        b.TotalPnL += trade.PnL
        if trade.PnL >= 0 {
                b.WinCount++
        } else {
                b.LossCount++
        }

        b.Trades = append(b.Trades, trade)
        if len(b.Trades) > 100 {
                b.Trades = b.Trades[len(b.Trades)-100:]
        }
        b.OpenTrade = nil

        if b.OnTradeClose != nil {
                b.OnTradeClose(TradeEvent{Bot: b, Trade: trade})
        }
}

// ─── Multi-timeframe confirmation ─────────────────────────────────────────────

// ConfirmHigherTF memeriksa apakah trend pada timeframe yang lebih tinggi
// selaras dengan arah sinyal entry.
//
// Implementasi: ambil closes dari setiap 6 candle (≈ H1-equivalent dari M10 candle)
// lalu bandingkan EMA9 vs EMA21. Jika data tidak cukup, return true (jangan blokir).
//
//   symbol          – pasangan forex (e.g. "EURUSD")
//   signalDirection – "BUY" atau "SELL"
//   mkt             – data market aktif
func ConfirmHigherTF(symbol, signalDirection string, mkt *market.Market) bool {
        htfCloses := mkt.GetHigherTFCloses(symbol, 6)
        if len(htfCloses) < 22 {
                return true // data tidak cukup — jangan blokir trade
        }
        ema9 := indicator.EMA(htfCloses, 9)
        ema21 := indicator.EMA(htfCloses, 21)
        if ema9 == 0 || ema21 == 0 {
                return true
        }
        switch signalDirection {
        case "BUY":
                return ema9 > ema21 // H1 trend up
        case "SELL":
                return ema9 < ema21 // H1 trend down
        }
        return true
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// pnlPercent menghitung P&L sebagai persentase dari harga entry.
// Positif = profit, negatif = loss.
func pnlPercent(side TradeSide, entry, current float64) float64 {
        if entry == 0 {
                return 0
        }
        if side == Buy {
                return (current - entry) / entry
        }
        return (entry - current) / entry
}

func (b *Bot) StatusLine() string {
        status := "● STOPPED"
        if b.IsRunning {
                status = "● RUNNING"
        }
        return fmt.Sprintf("%s | %s | %s | P&L: %+.2f USD | Win: %.0f%%",
                status, b.Symbol, b.Strategy, b.TotalPnL, b.WinRate())
}

func DefaultBots() []*Bot {
        return []*Bot{
                NewBot(1, "EUR Scalper", "EURUSD", Scalping, 1.0, 1.5, 0.8),
                NewBot(2, "GBP Trend", "GBPUSD", TrendFollowing, 1.5, 3.0, 1.5),
                NewBot(3, "JPY Reversal", "USDJPY", MeanReversion, 1.0, 2.0, 1.0),
        }
}
