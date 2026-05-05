# Finex CLI — MT5 Trading Bot

Terminal UI (TUI) trading bot for MetaTrader 5, designed for **FinexBisnisSolusi** broker. Built with Go + Bubble Tea.

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go) ![MT5](https://img.shields.io/badge/MT5-Demo%2FLive-orange) ![License](https://img.shields.io/badge/license-MIT-blue)

---

## Features

- **Live indicator dashboard** — RSI(7) gauge bar, Bollinger %B position, composite signal per pair (LONG ↑ / SHORT ↓ / WAIT –), updated every second
- **4 real trading strategies** — Scalping (RSI 7), Swing (Bollinger Bands), Trend Following (EMA crossover), Mean Reversion (RSI + BB)
- **8 forex pairs** — EURUSD, GBPUSD, USDJPY, AUDUSD, USDCAD, USDCHF, EURGBP, EURJPY
- **Multi-bot management** — create, edit, start/stop, delete bots via TUI; config persisted to `bots.json`
- **MT5 connection** — TLS + SRP-6a authentication to MetaQuotes trade server binary protocol
- **Structured logging** — all trades, bot events, and MT5 errors written to `finex-bot.log`

### H. Smart Position Sizing — Correlation-Aware (NEW)

File: `internal/risk/correlation.go`

Static 15-minute correlation matrix for all 8 pairs:

| Pair 1 | Pair 2 | Correlation |
|---|---|---|
| EURUSD | GBPUSD | +0.85 (high) |
| EURUSD | USDCHF | −0.92 (inverse) |
| USDJPY | EURJPY | +0.78 |
| EURUSD | EURJPY | +0.72 |
| GBPUSD | EURGBP | −0.75 |
| USDJPY | USDCHF | +0.70 |

Rules enforced before each trade entry:

1. **Correlation ≥ 0.7 → reduce total risk 30%** when adding a same-direction position in a highly correlated pair.
2. **Block conflicting positions**: opening BUY EURUSD while GBPUSD is also BUY when corr = +0.85 is allowed (same direction, reduced risk), but opening SELL EURUSD alongside BUY GBPUSD is blocked outright.
3. **Total correlated exposure ≤ 5% of equity** — if the combined risk % of highly-correlated open positions exceeds this cap, no new position is opened.

### I. Automatic Market Regime Detection (NEW)

File: `internal/market/regime.go`

Regimes are detected per-symbol using ADX, EMA slope, Bollinger Band width, and ATR. Cache TTL is 1 hour; forced refresh runs hourly.

| Regime | Detection Criteria | Strategy Weights |
|---|---|---|
| **TRENDING** | ADX > 25 + EMA slope ≠ 0 | Trend Following + Swing = 70% |
| **RANGING** | ADX < 20 + BB width < 1% | Mean Reversion + Scalping = 70% |
| **VOLATILE** | ATR(14) > 1.5× mean ATR(50) | All strategies at 50% (risk halved) |

ADX indicator added to `internal/indicator/indicator.go` using Wilder's smoothing.

### J. Performance Metrics Dashboard — Tab 6 (NEW)

New tab accessible via `6` or `Tab` key in the TUI.

Real-time metrics updated every 30 seconds:

- **Win Rate** — aggregate across all bots
- **Profit Factor** — sum of winning P&L / |sum of losing P&L|
- **Sharpe Ratio** — simplified per-trade Sharpe (mean / std × √n)
- **Max Drawdown** — peak-to-trough percentage from cumulative P&L curve
- **Top Pairs** — sorted by total realised P&L
- **Best Strategy** — sorted by profit factor per strategy
- **Risk Meter** — current drawdown vs maximum allowed; turns red if drawdown > 70% of limit

---

## Quick Start

### Prerequisites

- Go 1.21+
- MetaTrader 5 account (demo or live) from [Finex](https://finexindo.co.id)

### Run

```bash
git clone https://github.com/your-username/finex-cli.git
cd finex-cli
cp .env.example .env          # fill in your MT5 credentials
go build -o finex-bot .
./finex-bot
```

### Environment Variables

| Variable | Description | Example |
|---|---|---|
| `FINEX_LOGIN` | MT5 account number | `61369797` |
| `FINEX_PASSWORD` | MT5 account password | *(secret)* |
| `FINEX_SERVER` | MT5 server name | `FinexBisnisSolusi-Demo` |
| `FINEX_HOST` | MT5 server host:port | `prod-mt5-demo1.fnx.xmt.mx:443` |
| `FINEX_COMPANY` | Broker company name | `FinexBisnisSolusi` |

> **Never commit your `.env` file.** It is already listed in `.gitignore`.

---

## Keyboard Shortcuts

| Key | Action |
|---|---|
| `Tab` / `1`–`6` | Switch tabs |
| `s` | Start / stop selected bot |
| `n` | New bot |
| `e` | Edit selected bot |
| `d` | Delete selected bot |
| `↑` / `↓` | Navigate list |
| `q` / `Ctrl+C` | Quit |

---

## Project Structure

```
finex-cli/
├── main.go                        # TUI entry point (Bubble Tea model + views)
├── internal/
│   ├── account/account.go         # Account types (Demo / Real)
│   ├── bot/bot.go                 # Bot logic, trade lifecycle, default bots
│   ├── bot/risk.go                # RiskLimits, RiskProfile, dynamic lot sizing
│   ├── config/config.go           # Persist bot config to bots.json
│   ├── indicator/indicator.go     # RSI, EMA, SMA, Bollinger Bands, ATR, ADX, signals
│   ├── logger/logger.go           # Structured file logger (finex-bot.log)
│   ├── market/market.go           # Simulated forex market + candle history
│   ├── market/regime.go           # [NEW] Market regime detection (ADX/ATR/BB)
│   ├── risk/correlation.go        # [NEW] Correlation-aware position sizing
│   └── mt5/
│       ├── client.go              # MT5 TLS connection + SRP-6a auth
│       ├── account.go             # Binary account info parser
│       ├── proto.go               # MT5 binary packet encoding/decoding
│       └── srp6a.go               # SRP-6a (RFC 5054 Group 14 / SHA-256)
├── scripts/
│   └── post-merge.sh              # Post-merge build hook
├── .env.example                   # Environment variable template
└── .gitignore
```

---

## Trading Strategies

| Strategy | Indicator | Entry | Exit |
|---|---|---|---|
| **Scalping** | RSI(7) | RSI < 38 → BUY, RSI > 62 → SELL | TP 1.5% / SL 0.8% |
| **Swing Trading** | Bollinger Bands(20, 2σ) | Price ≤ lower → BUY, ≥ upper → SELL | TP 3.0% / SL 1.5% |
| **Trend Following** | EMA(9) × EMA(21) | Golden cross → BUY, death cross → SELL | TP 3.0% / SL 1.5% + reversal exit |
| **Mean Reversion** | RSI(14) + BB(20, 1.5σ) | RSI < 35 or price ≤ lower → BUY | TP 2.0% / SL 1.0% |

---

## Risk Management

| Feature | Details |
|---|---|
| Daily loss limit | Bot stops when daily loss % exceeds threshold |
| Max drawdown | Bot stops when trailing drawdown exceeds threshold |
| Dynamic lot sizing | Wilder ATR-based SL/TP + Kelly-inspired lot calc |
| Correlation filter | Blocks conflicting positions; reduces risk 30% on same-dir correlated pairs |
| Regime detection | Halves risk in volatile markets; weights strategies by regime |
| Consecutive loss cooldown | 1-hour trading halt after 5 consecutive losses |

---

## Real Trading Setup

To switch from demo to live trading, update these environment variables:

```bash
FINEX_LOGIN=<your_live_account_number>
FINEX_PASSWORD=<your_live_password>
FINEX_SERVER=FinexBisnisSolusi-Live
FINEX_HOST=<live_server_host>:443   # ask Finex support
```

> **Note:** Real order placement is not yet implemented. The bot currently authenticates to MT5 and runs strategies in simulation mode. Live order execution is planned for a future release.

---

## License

MIT — see [LICENSE](LICENSE) for details.
