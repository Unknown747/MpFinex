// Package optimizer menyediakan Genetic Algorithm untuk mencari parameter teknikal
// optimal per pasangan forex + strategy. Jalankan via: ./finex-bot --optimize EURUSD
package optimizer

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/finex/finex-cli/internal/indicator"
	"github.com/finex/finex-cli/internal/market"
)

// ─── Konstanta GA ─────────────────────────────────────────────────────────────

const (
	populationSize = 20
	numGenerations = 10
	mutationRate   = 0.15 // probabilitas mutasi per parameter
	tournamentK    = 3    // ukuran tournament selection

	// SL/TP untuk simulasi backtest (persentase dari entry price)
	backtestSLPct = 0.005 // 0.5%
	backtestTPPct = 0.010 // 1.0%

	OutputFile = "optimized_params.json"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Params menyimpan set parameter teknikal yang dioptimalkan untuk satu kombinasi
// simbol + strategy.
type Params struct {
	RSIPeriod int     `json:"rsi_period"` // periode RSI (7–20)
	RSIBuy    float64 `json:"rsi_buy"`    // threshold oversold: RSI < ini → BUY
	RSISell   float64 `json:"rsi_sell"`   // threshold overbought: RSI > ini → SELL
	EMAFast   int     `json:"ema_fast"`   // periode EMA cepat (5–20)
	EMASlow   int     `json:"ema_slow"`   // periode EMA lambat (15–50, selalu > EMAFast)
	BBPeriod  int     `json:"bb_period"`  // periode Bollinger Bands (10–30)
	BBMult    float64 `json:"bb_mult"`    // multiplier standar deviasi BB (1.5–3.0)
}

// OptimizedResult menyimpan params terbaik beserta metrik performa backtest-nya.
type OptimizedResult struct {
	Symbol       string  `json:"symbol"`
	Strategy     string  `json:"strategy"`
	Params       Params  `json:"params"`
	Fitness      float64 `json:"fitness"`
	WinRate      float64 `json:"win_rate"`
	ProfitFactor float64 `json:"profit_factor"`
}

// AllResults adalah format file optimized_params.json.
// Key: "EURUSD_Scalping", "GBPUSD_Trend Following", dll.
type AllResults map[string]OptimizedResult

// chromosome adalah representasi internal satu set parameter dalam populasi GA.
type chromosome struct {
	p       Params
	fitness float64
}

// ─── Random Helpers ───────────────────────────────────────────────────────────

func randInt(rng *rand.Rand, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return rng.Intn(hi-lo+1) + lo
}

func randFloat(rng *rand.Rand, lo, hi float64) float64 {
	return lo + rng.Float64()*(hi-lo)
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// randomChromosome menghasilkan chromosome dengan parameter acak dalam batas yang valid.
func randomChromosome(rng *rand.Rand) chromosome {
	emaFast := randInt(rng, 5, 20)
	emaSlow := randInt(rng, intMax(emaFast+1, 15), 50) // EMASlow selalu > EMAFast
	return chromosome{p: Params{
		RSIPeriod: randInt(rng, 7, 20),
		RSIBuy:    randFloat(rng, 20, 40),
		RSISell:   randFloat(rng, 60, 80),
		EMAFast:   emaFast,
		EMASlow:   emaSlow,
		BBPeriod:  randInt(rng, 10, 30),
		BBMult:    randFloat(rng, 1.5, 3.0),
	}}
}

// ─── Backtest ─────────────────────────────────────────────────────────────────

type btResult struct {
	wins, losses int
	grossProfit  float64
	grossLoss    float64
	maxDD        float64 // max drawdown percent
}

func (r btResult) profitFactor() float64 {
	if r.grossLoss == 0 {
		if r.grossProfit > 0 {
			return 10.0 // cap di 10x jika tanpa loss sama sekali
		}
		return 0
	}
	pf := r.grossProfit / r.grossLoss
	if pf > 10 {
		return 10
	}
	return pf
}

func (r btResult) winRate() float64 {
	total := r.wins + r.losses
	if total == 0 {
		return 0
	}
	return float64(r.wins) / float64(total) * 100
}

// calcFitness = profit_factor × win_rate / max_drawdown.
// Chromosomes dengan < 5 trade didiskon (terlalu sedikit data untuk dipercaya).
func (r btResult) calcFitness() float64 {
	pf := r.profitFactor()
	wr := r.winRate() / 100
	dd := math.Max(1.0, r.maxDD)
	score := pf * wr / dd
	total := r.wins + r.losses
	if total < 5 {
		score *= float64(total) / 5.0
	}
	return score
}

// backtest menjalankan simulasi walk-forward pada candles menggunakan params p
// untuk strategy yang diberikan.
func backtest(candles []market.Candle, strategy string, p Params) btResult {
	var res btResult
	if len(candles) < 60 {
		return res
	}

	type pos struct {
		side  string
		entry float64
	}
	var open *pos
	equity := 10000.0
	peak := equity

	for i := 55; i < len(candles); i++ {
		closes := extractCloses(candles, i+1)
		price := closes[len(closes)-1]

		if open != nil {
			var closed bool
			if open.side == "BUY" {
				if price <= open.entry*(1-backtestSLPct) {
					res.losses++
					loss := open.entry * backtestSLPct
					res.grossLoss += loss
					equity -= loss
					closed = true
				} else if price >= open.entry*(1+backtestTPPct) {
					res.wins++
					profit := open.entry * backtestTPPct
					res.grossProfit += profit
					equity += profit
					closed = true
				}
			} else {
				if price >= open.entry*(1+backtestSLPct) {
					res.losses++
					loss := open.entry * backtestSLPct
					res.grossLoss += loss
					equity -= loss
					closed = true
				} else if price <= open.entry*(1-backtestTPPct) {
					res.wins++
					profit := open.entry * backtestTPPct
					res.grossProfit += profit
					equity += profit
					closed = true
				}
			}
			if closed {
				if equity > peak {
					peak = equity
				}
				if peak > 0 {
					dd := (peak - equity) / peak * 100
					if dd > res.maxDD {
						res.maxDD = dd
					}
				}
				open = nil
			}
			continue
		}

		sig := signalForParams(closes, strategy, p)
		if sig != "" {
			open = &pos{side: sig, entry: price}
		}
	}
	return res
}

// extractCloses mengambil Close prices dari slice candle (index 0..n-1).
func extractCloses(candles []market.Candle, n int) []float64 {
	if n > len(candles) {
		n = len(candles)
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = candles[i].Close
	}
	return out
}

// signalForParams menghasilkan sinyal "BUY", "SELL", atau "" berdasarkan strategy dan params.
func signalForParams(closes []float64, strategy string, p Params) string {
	n := len(closes)
	if n < 2 {
		return ""
	}
	price := closes[n-1]

	switch strategy {
	case "Scalping":
		rsi := indicator.RSI(closes, p.RSIPeriod)
		if rsi < p.RSIBuy {
			return "BUY"
		}
		if rsi > p.RSISell {
			return "SELL"
		}

	case "Trend Following":
		if n < p.EMASlow+2 {
			return ""
		}
		fast := indicator.EMA(closes, p.EMAFast)
		slow := indicator.EMA(closes, p.EMASlow)
		pFast := indicator.EMA(closes[:n-1], p.EMAFast)
		pSlow := indicator.EMA(closes[:n-1], p.EMASlow)
		if fast == 0 || slow == 0 || pFast == 0 || pSlow == 0 {
			return ""
		}
		if pFast <= pSlow && fast > slow {
			return "BUY"
		}
		if pFast >= pSlow && fast < slow {
			return "SELL"
		}

	case "Swing Trading":
		_, upper, lower := indicator.BollingerBands(closes, p.BBPeriod, p.BBMult)
		if upper == 0 {
			return ""
		}
		if price <= lower {
			return "BUY"
		}
		if price >= upper {
			return "SELL"
		}

	case "Mean Reversion":
		rsi := indicator.RSI(closes, p.RSIPeriod)
		_, upper, lower := indicator.BollingerBands(closes, p.BBPeriod, p.BBMult)
		if rsi < p.RSIBuy || (upper > 0 && price <= lower) {
			return "BUY"
		}
		if rsi > p.RSISell || (upper > 0 && price >= upper) {
			return "SELL"
		}
	}
	return ""
}

// ─── GA Operators ─────────────────────────────────────────────────────────────

// evaluate menghitung fitness untuk setiap chromosome dalam populasi.
func evaluate(pop []chromosome, candles []market.Candle, strategy string) {
	for i := range pop {
		res := backtest(candles, strategy, pop[i].p)
		pop[i].fitness = res.calcFitness()
	}
}

// best mengembalikan chromosome dengan fitness tertinggi.
func best(pop []chromosome) chromosome {
	b := pop[0]
	for _, c := range pop[1:] {
		if c.fitness > b.fitness {
			b = c
		}
	}
	return b
}

// tournamentSelect memilih satu chromosome via tournament selection.
func tournamentSelect(pop []chromosome, rng *rand.Rand) chromosome {
	winner := pop[rng.Intn(len(pop))]
	for i := 1; i < tournamentK; i++ {
		c := pop[rng.Intn(len(pop))]
		if c.fitness > winner.fitness {
			winner = c
		}
	}
	return winner
}

// crossover menggabungkan dua parent menjadi satu child (single-point crossover).
// Titik crossover: RSI params (a) + EMA/BB params (b) atau sebaliknya.
func crossover(a, b chromosome, rng *rand.Rand) chromosome {
	if rng.Float64() < 0.5 {
		return chromosome{p: Params{
			RSIPeriod: a.p.RSIPeriod,
			RSIBuy:    a.p.RSIBuy,
			RSISell:   a.p.RSISell,
			EMAFast:   b.p.EMAFast,
			EMASlow:   b.p.EMASlow,
			BBPeriod:  b.p.BBPeriod,
			BBMult:    b.p.BBMult,
		}}
	}
	return chromosome{p: Params{
		RSIPeriod: b.p.RSIPeriod,
		RSIBuy:    b.p.RSIBuy,
		RSISell:   b.p.RSISell,
		EMAFast:   a.p.EMAFast,
		EMASlow:   a.p.EMASlow,
		BBPeriod:  a.p.BBPeriod,
		BBMult:    a.p.BBMult,
	}}
}

// mutate mengaplikasikan mutasi acak per parameter dengan probabilitas mutationRate.
func mutate(c chromosome, rng *rand.Rand) chromosome {
	p := c.p
	if rng.Float64() < mutationRate {
		p.RSIPeriod = randInt(rng, 7, 20)
	}
	if rng.Float64() < mutationRate {
		p.RSIBuy = randFloat(rng, 20, 40)
	}
	if rng.Float64() < mutationRate {
		p.RSISell = randFloat(rng, 60, 80)
	}
	if rng.Float64() < mutationRate {
		newFast := randInt(rng, 5, 20)
		p.EMAFast = newFast
		if p.EMASlow <= newFast {
			p.EMASlow = randInt(rng, newFast+1, 50)
		}
	}
	if rng.Float64() < mutationRate {
		p.EMASlow = randInt(rng, intMax(p.EMAFast+1, 15), 50)
	}
	if rng.Float64() < mutationRate {
		p.BBPeriod = randInt(rng, 10, 30)
	}
	if rng.Float64() < mutationRate {
		p.BBMult = randFloat(rng, 1.5, 3.0)
	}
	return chromosome{p: p}
}

// ─── Public API ───────────────────────────────────────────────────────────────

// Optimize menjalankan Genetic Algorithm untuk menemukan parameter teknikal terbaik
// bagi kombinasi simbol + strategy yang diberikan.
//
// Konfigurasi:
//   - Populasi: 20 chromosome
//   - Generasi: 10
//   - Fitness: profit_factor × win_rate / max_drawdown
//   - Elitism: chromosome terbaik selalu dipertahankan ke generasi berikutnya
func Optimize(symbol, strategy string, candles []market.Candle) OptimizedResult {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Inisialisasi populasi awal secara acak
	pop := make([]chromosome, populationSize)
	for i := range pop {
		pop[i] = randomChromosome(rng)
	}
	evaluate(pop, candles, strategy)

	// Jalankan evolusi selama numGenerations generasi
	for gen := 0; gen < numGenerations; gen++ {
		next := make([]chromosome, populationSize)
		next[0] = best(pop) // elitism: pertahankan yang terbaik
		for i := 1; i < populationSize; i++ {
			p1 := tournamentSelect(pop, rng)
			p2 := tournamentSelect(pop, rng)
			child := crossover(p1, p2, rng)
			child = mutate(child, rng)
			next[i] = child
		}
		evaluate(next, candles, strategy)
		pop = next
	}

	winner := best(pop)
	bt := backtest(candles, strategy, winner.p)
	return OptimizedResult{
		Symbol:       symbol,
		Strategy:     strategy,
		Params:       winner.p,
		Fitness:      winner.fitness,
		WinRate:      bt.winRate(),
		ProfitFactor: bt.profitFactor(),
	}
}

// SaveResults menyimpan slice OptimizedResult ke file optimized_params.json.
// Data di-merge ke file yang sudah ada (tidak overwrite hasil simbol/strategy lain).
func SaveResults(results []OptimizedResult) error {
	existing := make(AllResults)
	if data, err := os.ReadFile(OutputFile); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	for _, r := range results {
		key := fmt.Sprintf("%s_%s", r.Symbol, r.Strategy)
		existing[key] = r
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}
	return os.WriteFile(OutputFile, data, 0644)
}
