# Finex CLI — Project Overview

## Architecture

Pure Go TUI application (no web server). Single binary `finex-bot` built from root `main.go`.

```
main.go                        # Bubble Tea TUI — model, update, view
internal/
  account/account.go           # Account struct (Demo/Real), balance tracking
  bot/bot.go                   # Bot struct, Tick(), trade lifecycle, DefaultBots()
  config/config.go             # SaveBots/LoadBots — persists to bots.json
  indicator/indicator.go       # RSI, EMA, SMA, BollingerBands, signal functions
  logger/logger.go             # Thread-safe structured logger → finex-bot.log
  market/market.go             # Simulated forex market, candle history, GetCloses()
  mt5/
    client.go                  # TLS connect + SRP-6a handshake to MT5 server
    account.go                 # Binary account info parser (little-endian)
    proto.go                   # MT5 binary packet read/write + helpers
    srp6a.go                   # SRP-6a RFC 5054 Group 14 / SHA-256 implementation
scripts/
  post-merge.sh                # Post-merge hook: go build -o finex-bot .
```

## Run Command

```bash
go build -o finex-bot . && ./finex-bot
```

Workflow name: **Finex CLI** (console output type).

## Environment Variables

Set via Replit Secrets / userenv:

| Variable | Where | Notes |
|---|---|---|
| `FINEX_LOGIN` | userenv.shared | MT5 account number (not secret) |
| `FINEX_COMPANY` | userenv.shared | Broker name |
| `FINEX_SERVER` | userenv.shared | MT5 server name |
| `FINEX_HOST` | userenv.shared | MT5 host:port |
| `FINEX_PASSWORD` | Replit Secret | Never in plaintext |

## Key Design Decisions

- **No crypto** — all pairs are forex majors + crosses (EURUSD, GBPUSD, USDJPY, AUDUSD, USDCAD, USDCHF, EURGBP, EURJPY)
- **Volatility calibration** — 0.001 per tick ≈ 10 pip per tick ≈ realistic 10-second candle range for simulation
- **Exit strategy per strategy type** — only TrendFollowing uses reversal exit; Scalping/Swing/MeanReversion are TP/SL only
- **RSI thresholds** — Scalping uses 38/62 (not classic 30/70) for adequate signal frequency at forex volatility
- **MeanReversion** uses OR logic (RSI < 35 OR price ≤ lower BB) — AND was too strict for intraday forex
- **History generation** — starts from basePrice (not 0.95×) with 5× volatility to avoid RSI upward bias
- **bots.json** — gitignored, per-user state, auto-created on first bot save
- **MT5 connection** — authenticates via SRP-6a but does NOT place real orders (simulation only)

## Dependencies

```
github.com/charmbracelet/bubbles   — text input component
github.com/charmbracelet/bubbletea — Elm-architecture TUI framework
github.com/charmbracelet/lipgloss  — terminal styling
```

## Tab Structure

1. **Dashboard** — account summary, running bots, open trades, recent activity
2. **Markets** — live RSI gauge + Bollinger %B per pair + composite signal + EURUSD sparkline
3. **Bots** — bot list, start/stop (s), new (n), edit (e), delete (d)
4. **Trades** — full trade history with PnL, side, status per bot
5. **Settings** — MT5 connection status, account info, debug log

## Files Excluded from Git

- `finex-bot` — compiled binary
- `finex-bot.log` — session log
- `bots.json` — user's saved bot configs
- `.env` — local credentials (use `.env.example` as template)
- `.cache/`, `.local/` — Replit internal caches
