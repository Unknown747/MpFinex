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
        prices    map[string]*Price
        history   map[string][]Candle
        tickCount map[string]int // ticks since last candle close
        rng       *rand.Rand
}

var defaultPrices = map[string]float64{
        "EURUSD": 1.0850,
        "GBPUSD": 1.2650,
        "USDJPY": 154.50,
        "AUDUSD": 0.6450,
        "USDCAD": 1.3750,
        "USDCHF": 0.9050,
        "EURGBP": 0.8580,
        "EURJPY": 167.50,
}

var volatility = map[string]float64{
        "EURUSD": 0.00030,
        "GBPUSD": 0.00040,
        "USDJPY": 0.03000,
        "AUDUSD": 0.00035,
        "USDCAD": 0.00030,
        "USDCHF": 0.00028,
        "EURGBP": 0.00022,
        "EURJPY": 0.04000,
}

// candleIntervalTicks is how many 1-second ticks make up one candle.
// 10 ticks = 10-second candles, giving indicators enough data quickly.
const candleIntervalTicks = 10

func NewMarket() *Market {
        m := &Market{
                prices:    make(map[string]*Price),
                history:   make(map[string][]Candle),
                tickCount: make(map[string]int),
                rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
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
        candles := make([]Candle, 100)
        p := basePrice * 0.95
        now := time.Now()
        for i := 0; i < 100; i++ {
                vol := volatility[symbol]
                change := (m.rng.Float64()*2 - 1) * vol * p
                o := p
                c := p + change
                h := math.Max(o, c) * (1 + m.rng.Float64()*vol*0.5)
                l := math.Min(o, c) * (1 - m.rng.Float64()*vol*0.5)
                candles[i] = Candle{
                        Timestamp: now.Add(time.Duration(-100+i) * time.Minute * candleIntervalTicks),
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

                // Update current (forming) candle
                candles := m.history[sym]
                idx := len(candles) - 1
                if newPrice > candles[idx].High {
                        candles[idx].High = newPrice
                }
                if newPrice < candles[idx].Low {
                        candles[idx].Low = newPrice
                }
                candles[idx].Close = newPrice

                // Close candle and open a new one every candleIntervalTicks
                m.tickCount[sym]++
                if m.tickCount[sym] >= candleIntervalTicks {
                        m.tickCount[sym] = 0
                        newCandle := Candle{
                                Timestamp: time.Now(),
                                Open:      newPrice,
                                High:      newPrice,
                                Low:       newPrice,
                                Close:     newPrice,
                                Volume:    float64(m.rng.Intn(1000) + 100),
                        }
                        m.history[sym] = append(m.history[sym], newCandle)
                        // Keep at most 200 candles to bound memory
                        if len(m.history[sym]) > 200 {
                                m.history[sym] = m.history[sym][len(m.history[sym])-200:]
                        }
                }
        }
}

func (m *Market) GetPrice(symbol string) *Price {
        return m.prices[symbol]
}

func (m *Market) GetAllPrices() []*Price {
        symbols := []string{
                "EURUSD", "GBPUSD", "USDJPY", "AUDUSD",
                "USDCAD", "USDCHF", "EURGBP", "EURJPY",
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

// GetCloses returns only the Close prices from the candle history,
// ready to feed into indicator calculations.
func (m *Market) GetCloses(symbol string) []float64 {
        candles := m.history[symbol]
        out := make([]float64, len(candles))
        for i, c := range candles {
                out[i] = c.Close
        }
        return out
}
