package market

import (
	"math"
	"math/rand"
	"time"
)

type Candle struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

type Price struct {
	Symbol      string
	Price       float64
	Prev        float64
	Change      float64
	ChangePct   float64
	High24h     float64
	Low24h      float64
	Volume24h   float64
	LastUpdated time.Time
}

func (p *Price) Direction() string {
	if p.Change > 0 {
		return "▲"
	} else if p.Change < 0 {
		return "▼"
	}
	return "─"
}

type Market struct {
	prices  map[string]*Price
	history map[string][]Candle
	rng     *rand.Rand
}

var defaultPrices = map[string]float64{
	"BTC/USDT":  67450.00,
	"ETH/USDT":  3520.00,
	"BNB/USDT":  415.00,
	"XRP/USDT":  0.6234,
	"SOL/USDT":  178.50,
	"ADA/USDT":  0.4521,
	"DOGE/USDT": 0.1823,
	"AVAX/USDT": 38.72,
}

var volatility = map[string]float64{
	"BTC/USDT":  0.0018,
	"ETH/USDT":  0.0022,
	"BNB/USDT":  0.0019,
	"XRP/USDT":  0.0025,
	"SOL/USDT":  0.0028,
	"ADA/USDT":  0.0030,
	"DOGE/USDT": 0.0035,
	"AVAX/USDT": 0.0032,
}

func NewMarket() *Market {
	m := &Market{
		prices:  make(map[string]*Price),
		history: make(map[string][]Candle),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for sym, price := range defaultPrices {
		m.prices[sym] = &Price{
			Symbol:    sym,
			Price:     price,
			Prev:      price,
			High24h:   price * 1.035,
			Low24h:    price * 0.965,
			Volume24h: price * float64(m.rng.Intn(50000)+10000),
		}
		m.generateHistory(sym, price)
	}
	return m
}

func (m *Market) generateHistory(symbol string, basePrice float64) {
	candles := make([]Candle, 50)
	p := basePrice * 0.95
	now := time.Now()
	for i := 0; i < 50; i++ {
		vol := volatility[symbol]
		change := (m.rng.Float64()*2 - 1) * vol * p
		o := p
		c := p + change
		h := math.Max(o, c) * (1 + m.rng.Float64()*vol*0.5)
		l := math.Min(o, c) * (1 - m.rng.Float64()*vol*0.5)
		candles[i] = Candle{
			Timestamp: now.Add(time.Duration(-50+i) * time.Hour),
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
			Volume:    float64(m.rng.Intn(1000) + 100),
		}
		p = c
	}
	m.history[symbol] = candles
}

func (m *Market) Tick() {
	for sym, p := range m.prices {
		vol := volatility[sym]
		drift := (m.rng.Float64()*2 - 1) * vol * p.Price
		newPrice := p.Price + drift
		if newPrice < 0.001 {
			newPrice = 0.001
		}
		p.Prev = p.Price
		p.Price = newPrice
		p.Change = newPrice - p.Prev
		p.ChangePct = (p.Change / p.Prev) * 100
		if newPrice > p.High24h {
			p.High24h = newPrice
		}
		if newPrice < p.Low24h {
			p.Low24h = newPrice
		}
		p.LastUpdated = time.Now()

		candles := m.history[sym]
		last := candles[len(candles)-1]
		if newPrice > last.High {
			candles[len(candles)-1].High = newPrice
		}
		if newPrice < last.Low {
			candles[len(candles)-1].Low = newPrice
		}
		candles[len(candles)-1].Close = newPrice
	}
}

func (m *Market) GetPrice(symbol string) *Price {
	return m.prices[symbol]
}

func (m *Market) GetAllPrices() []*Price {
	symbols := []string{
		"BTC/USDT", "ETH/USDT", "BNB/USDT", "XRP/USDT",
		"SOL/USDT", "ADA/USDT", "DOGE/USDT", "AVAX/USDT",
	}
	out := make([]*Price, 0, len(symbols))
	for _, sym := range symbols {
		if p, ok := m.prices[sym]; ok {
			out = append(out, p)
		}
	}
	return out
}

func (m *Market) GetHistory(symbol string) []Candle {
	return m.history[symbol]
}
