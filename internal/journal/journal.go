// Package journal persists every closed trade to a JSON Lines file and
// generates an equity curve HTML report (equity_chart.html) every 50 trades.
//
// Data model:
//
//	TradeRecord  — one closed trade (matches spec fields exactly)
//	Journal      — thread-safe append log + equity curve generator
//
// File layout:
//
//	trade_journal.jsonl — one JSON object per line (easy to tail / grep)
//	equity_chart.html   — self-contained Chart.js page regenerated every N trades
package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// equityGenInterval controls how often the equity curve HTML is regenerated.
const equityGenInterval = 50

// ─── Trade Record ─────────────────────────────────────────────────────────────

// TradeRecord is a complete, immutable snapshot of a single closed trade.
// Every field matches the spec; zero values mean the data was unavailable
// (e.g. SlippagePips is 0 on live orders where slippage isn't tracked).
type TradeRecord struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Symbol         string    `json:"symbol"`
	Strategy       string    `json:"strategy"`
	Direction      string    `json:"direction"`
	EntryPrice     float64   `json:"entry_price"`
	ExitPrice      float64   `json:"exit_price"`
	ProfitPips     float64   `json:"profit_pips"`
	ProfitUSD      float64   `json:"profit_usd"`
	RiskPercent    float64   `json:"risk_percent"`
	ExitReason     string    `json:"exit_reason"`
	SlippagePips   float64   `json:"slippage_pips"`
	HoldingMinutes float64   `json:"holding_minutes"`
	MAEPips        float64   `json:"mae_pips"`
	MFEPips        float64   `json:"mfe_pips"`
}

// ─── Internal stats accumulator ───────────────────────────────────────────────

type strategyStats struct {
	name     string
	wins     int
	losses   int
	totalPnL float64
	sumMAE   float64
	sumMFE   float64
}

// ─── Journal ──────────────────────────────────────────────────────────────────

// Journal records every closed trade and generates periodic performance reports.
// All public methods are safe to call from multiple goroutines.
type Journal struct {
	mu         sync.Mutex
	records    []TradeRecord
	filePath   string
	equityPath string
	equityBase float64 // initial account balance (equity curve origin)
}

// New opens (or creates) a Journal backed by filePath (JSON Lines).
// equityBase is the starting balance used as the curve's Y-axis origin.
// Existing records are loaded on startup; if any exist the equity curve is
// regenerated immediately in the background.
func New(filePath string, equityBase float64) *Journal {
	j := &Journal{
		filePath:   filePath,
		equityPath: "equity_chart.html",
		equityBase: equityBase,
	}
	j.load()
	if len(j.records) > 0 {
		go j.GenerateEquityCurve()
	}
	return j
}

// load reads all JSON Lines from filePath into memory (best-effort; parse
// errors on individual lines are silently skipped).
func (j *Journal) load() {
	data, err := os.ReadFile(j.filePath)
	if err != nil {
		return
	}
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			line := data[start:i]
			if len(line) > 0 {
				var r TradeRecord
				if json.Unmarshal(line, &r) == nil {
					j.records = append(j.records, r)
				}
			}
			start = i + 1
		}
	}
}

// Record appends r to the in-memory journal and flushes it to disk
// asynchronously. Every equityGenInterval trades the equity curve is
// regenerated in a background goroutine.
func (j *Journal) Record(r TradeRecord) {
	j.mu.Lock()
	j.records = append(j.records, r)
	count := len(j.records)
	j.mu.Unlock()

	go j.appendLine(r)

	if count%equityGenInterval == 0 {
		go j.GenerateEquityCurve()
	}
}

// appendLine marshals r to JSON and appends it (with a trailing newline) to
// the journal file. The file is opened in append mode so concurrent writes
// from multiple goroutines are safe on POSIX systems.
func (j *Journal) appendLine(r TradeRecord) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	f, err := os.OpenFile(j.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))
}

// ─── Equity Curve & MAE/MFE ───────────────────────────────────────────────────

// GenerateEquityCurve takes a snapshot of all records, computes the cumulative
// equity series and per-strategy MAE/MFE stats, then writes equity_chart.html.
// Safe to call concurrently — it works on a private copy of the records slice.
func (j *Journal) GenerateEquityCurve() {
	j.mu.Lock()
	records := make([]TradeRecord, len(j.records))
	copy(records, j.records)
	j.mu.Unlock()

	if len(records) == 0 {
		return
	}

	// Single pass: build equity series + per-strategy stats.
	equity := j.equityBase
	labels := make([]string, len(records))
	equities := make([]float64, len(records))
	stats := make(map[string]*strategyStats)

	for i, r := range records {
		equity += r.ProfitUSD
		labels[i] = fmt.Sprintf("%s #%d", r.Symbol, i+1)
		equities[i] = equity

		s, ok := stats[r.Strategy]
		if !ok {
			s = &strategyStats{name: r.Strategy}
			stats[r.Strategy] = s
		}
		if r.ProfitUSD >= 0 {
			s.wins++
		} else {
			s.losses++
		}
		s.totalPnL += r.ProfitUSD
		s.sumMAE += r.MAEPips
		s.sumMFE += r.MFEPips
	}

	finalEquity := equities[len(equities)-1]
	totalReturn := (finalEquity - j.equityBase) / j.equityBase * 100

	// Serialise data for Chart.js.
	labelsJS, equityJS := buildChartData(labels, equities)

	// Strategy summary table rows.
	statsRows := buildStatsRows(stats)

	html := buildHTML(j.equityBase, finalEquity, totalReturn, len(records), statsRows, labelsJS, equityJS)
	_ = os.WriteFile(j.equityPath, []byte(html), 0644)
}

// buildChartData converts Go slices into JavaScript array literals.
func buildChartData(labels []string, equities []float64) (labelsJS, equityJS string) {
	labelsJS = "["
	equityJS = "["
	for i, l := range labels {
		labelsJS += fmt.Sprintf("%q", l)
		equityJS += fmt.Sprintf("%.2f", equities[i])
		if i < len(labels)-1 {
			labelsJS += ","
			equityJS += ","
		}
	}
	labelsJS += "]"
	equityJS += "]"
	return
}

// buildStatsRows builds the HTML <tr> rows for the strategy summary table.
// MAE/MFE are averaged across all trades for the strategy (win + loss).
func buildStatsRows(stats map[string]*strategyStats) string {
	rows := ""
	for _, s := range stats {
		total := s.wins + s.losses
		wr, avgMAE, avgMFE := 0.0, 0.0, 0.0
		if total > 0 {
			wr = float64(s.wins) / float64(total) * 100
			avgMAE = s.sumMAE / float64(total)
			avgMFE = s.sumMFE / float64(total)
		}
		rows += fmt.Sprintf(
			"<tr><td>%s</td><td>%d/%d</td><td>%.1f%%</td><td>%+.2f</td><td>%.1f</td><td>%.1f</td></tr>",
			s.name, s.wins, total, wr, s.totalPnL, avgMAE, avgMFE,
		)
	}
	return rows
}

// buildHTML generates the full self-contained Chart.js equity curve page.
// All %% are doubled to escape them inside fmt.Sprintf.
func buildHTML(startEq, endEq, totalReturn float64, tradeCount int, statsRows, labelsJS, equityJS string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Finex — Equity Curve</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
  <style>
    body { font-family: monospace; background: #0d1117; color: #c9d1d9; margin: 0; padding: 16px; }
    h2   { color: #58a6ff; margin-bottom: 4px; }
    p    { color: #8b949e; margin: 0 0 16px; }
    canvas { width: 100%%; background: #161b22; border-radius: 8px; padding: 8px; }
    table  { border-collapse: collapse; margin-top: 24px; width: 100%%; }
    th, td { padding: 6px 12px; text-align: left; border-bottom: 1px solid #30363d; font-size: 13px; }
    th { color: #58a6ff; }
  </style>
</head>
<body>
  <h2>Finex Bot — Equity Curve</h2>
  <p>Start: $%.2f &nbsp;|&nbsp; End: $%.2f &nbsp;|&nbsp; Return: %+.2f%% &nbsp;|&nbsp; Trades: %d</p>
  <canvas id="ec" height="80"></canvas>
  <table>
    <thead>
      <tr>
        <th>Strategy</th><th>W / Total</th><th>Win%%</th>
        <th>P&amp;L (USD)</th><th>Avg MAE (pip)</th><th>Avg MFE (pip)</th>
      </tr>
    </thead>
    <tbody>%s</tbody>
  </table>
  <script>
  new Chart(document.getElementById('ec'), {
    type: 'line',
    data: {
      labels: %s,
      datasets: [{
        label: 'Equity (USD)',
        data: %s,
        borderColor: '#58a6ff',
        backgroundColor: 'rgba(88,166,255,0.08)',
        borderWidth: 1.5,
        pointRadius: 0,
        fill: true,
        tension: 0.3
      }]
    },
    options: {
      plugins: { legend: { labels: { color: '#8b949e' } } },
      scales: {
        x: { display: false },
        y: { ticks: { color: '#8b949e' }, grid: { color: '#21262d' } }
      }
    }
  });
  </script>
</body>
</html>`, startEq, endEq, totalReturn, tradeCount, statsRows, labelsJS, equityJS)
}
