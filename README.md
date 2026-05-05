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
| `Tab` / `1`–`5` | Switch tabs |
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
├── main.go                    # TUI entry point (Bubble Tea model + views)
├── internal/
│   ├── account/account.go     # Account types (Demo / Real)
│   ├── bot/bot.go             # Bot logic, trade lifecycle, default bots
│   ├── config/config.go       # Persist bot config to bots.json
│   ├── indicator/indicator.go # RSI, EMA, SMA, Bollinger Bands, signals
│   ├── logger/logger.go       # Structured file logger (finex-bot.log)
│   ├── market/market.go       # Simulated forex market + candle history
│   └── mt5/
│       ├── client.go          # MT5 TLS connection + SRP-6a auth
│       ├── account.go         # Binary account info parser
│       ├── proto.go           # MT5 binary packet encoding/decoding
│       └── srp6a.go           # SRP-6a (RFC 5054 Group 14 / SHA-256)
├── scripts/
│   └── post-merge.sh          # Post-merge build hook
├── .env.example               # Environment variable template
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

## Real Trading Setup

To switch from demo to live trading, update these environment variables:

```bash
FINEX_LOGIN=<your_live_account_number>
FINEX_PASSWORD=<your_live_password>
FINEX_SERVER=FinexBisnisSolusi-Live
FINEX_HOST=<live_server_host>:443   # ask Finex support
```

> **Note:** Real order placement is not yet implemented. The bot currently authenticates to MT5 and runs strategies in simulation mode. Live order execution (`PlaceOrder`, price feed subscription) is planned for a future release.

> **Network:** The MT5 trade server may block connections from cloud/VPS IP addresses. For live trading, run the bot from your own machine or a dedicated VPS with a whitelisted static IP.

---

## License

MIT — see [LICENSE](LICENSE) for details.
