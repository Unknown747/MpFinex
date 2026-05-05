package bot

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/finex/finex-cli/internal/market"
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

func (b *Bot) Tick(mkt *market.Market, accountBalance float64) {
	if !b.IsRunning {
		return
	}

	price := mkt.GetPrice(b.Symbol)
	if price == nil {
		return
	}

	if b.OpenTrade != nil {
		b.checkCloseCondition(price.Price)
		return
	}

	if b.shouldOpenTrade() {
		b.openTrade(price.Price, accountBalance)
	}
}

func (b *Bot) shouldOpenTrade() bool {
	switch b.Strategy {
	case Scalping:
		return b.rng.Intn(20) == 0
	case SwingTrading:
		return b.rng.Intn(40) == 0
	case TrendFollowing:
		return b.rng.Intn(30) == 0
	case MeanReversion:
		return b.rng.Intn(25) == 0
	}
	return false
}

func (b *Bot) openTrade(price, balance float64) {
	risk := balance * (b.RiskPct / 100)
	qty := risk / price

	side := Buy
	if b.rng.Intn(2) == 0 {
		side = Sell
	}

	b.tradeCounter++
	trade := &Trade{
		ID:         b.tradeCounter,
		BotID:      b.ID,
		Symbol:     b.Symbol,
		Side:       side,
		Quantity:   qty,
		EntryPrice: price,
		Status:     Open,
		OpenedAt:   time.Now(),
	}
	b.OpenTrade = trade

	if b.OnTradeOpen != nil {
		b.OnTradeOpen(TradeEvent{Bot: b, Trade: trade})
	}
}

func (b *Bot) checkCloseCondition(currentPrice float64) {
	if b.OpenTrade == nil {
		return
	}

	entry := b.OpenTrade.EntryPrice
	tp := b.TakeProfitPct / 100
	sl := b.StopLossPct / 100

	var pnlPct float64
	if b.OpenTrade.Side == Buy {
		pnlPct = (currentPrice - entry) / entry
	} else {
		pnlPct = (entry - currentPrice) / entry
	}

	shouldClose := pnlPct >= tp || pnlPct <= -sl

	if !shouldClose {
		var randomClose int
		switch b.Strategy {
		case Scalping:
			randomClose = 15
		case SwingTrading:
			randomClose = 60
		default:
			randomClose = 35
		}
		shouldClose = b.rng.Intn(randomClose) == 0
	}

	if shouldClose {
		b.closeTrade(currentPrice, pnlPct)
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
		NewBot(1, "Alpha Scalper", "BTC/USDT", Scalping, 1.0, 2.0, 1.0),
		NewBot(2, "Trend Rider", "ETH/USDT", TrendFollowing, 1.5, 3.0, 1.5),
		NewBot(3, "Mean Rev Pro", "SOL/USDT", MeanReversion, 1.0, 2.5, 1.2),
	}
}
