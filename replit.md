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

## Security Features (Bagian 1)

### A. Daily Loss Limit & B. Max Drawdown Trailing (`internal/bot/risk.go`)
- Struct `RiskLimits` dengan `MaxDailyLossPercent`, `MaxDrawdownPercent`, `InitialEquity`, `PeakEquity`, `DailyLoss`, `LastReset`
- `CheckDailyLoss(equity, bot, price)`: Reset tiap hari baru, bandingkan loss vs limit, tutup posisi + stop bot jika terlampaui
- `CheckDrawdown(equity, bot, price)`: Tracking peak equity, hitung drawdown%, tutup posisi + stop bot jika terlampaui
- Diaktifkan per-bot via `bot.Risk = bot.NewRiskLimits(maxDailyLoss%, maxDrawdown%, initialEquity)`
- Dipanggil di `bot.Tick()` sebelum setiap entry order baru

### C. Enkripsi Credentials (`internal/config/encrypt.go`)
- AES-256-GCM dengan key dari env var `ENCRYPTION_KEY` (wajib 32 byte)
- `EncryptCredentials(login, password, server)` → base64 ciphertext
- `DecryptCredentials(ciphertext)` → login, password, server
- Program exit dengan pesan error jika `ENCRYPTION_KEY` tidak ada atau bukan 32 byte

### D. Connection Watchdog (`internal/mt5/heartbeat.go`)
- `StartHeartbeat(client, interval, onPermanentFailure)` — goroutine background
- Kirim `CmdPing` setiap 3 detik; 2 gagal berturut-turut → trigger reconnect
- Exponential backoff reconnect: 1s → 2s → 4s → 8s, maksimal 3 percobaan
- Jika semua gagal: log "MT5 disconnected permanently", tutup semua posisi, shutdown TUI via `heartbeatShutdownMsg`
- Koneksi persisten disimpan di `client.conn` (thread-safe via `sync.Mutex`)
- Method baru di `mt5.Client`: `Ping() error`, `Disconnect()`

## Profitabilitas & Multi-TF (Bagian 2)

### A. Dynamic Lot Sizing (`internal/bot/risk.go`)
- Struct `SymbolInfo` dengan `PipSize`, `PipValue`, `MinLot`, `LotStep`, `ContractSize`
- `DefaultSymbolInfo` map untuk semua 8 pasangan forex yang didukung
- `CalculateLotSize(equity, riskPercent, stopLossPips, SymbolInfo) float64`
- Rumus: `lot = (equity × risk%) / (SL_pips × pip_value)`, dibulatkan ke LotStep
- Dipanggil di `openTrade` setelah ATR dihitung (non-Scalping), fallback ke `risk/price` untuk Scalping

### B. ATR-based Stop Loss & Take Profit (`internal/indicator/indicator.go`)
- `ATR(highs, lows, closes []float64, period int) float64` — Wilder's smoothed ATR
- Diaktifkan di `openTrade`: **SL = entry ± 1.5×ATR**, **TP = entry ± 3.0×ATR**
- Scalping tetap menggunakan fixed % SL/TP (tidak berubah)
- `GetHighLows(symbol)` ditambahkan ke `market.Market` untuk feed data ATR

### C. Multi-Timeframe Confirmation (`internal/bot/bot.go`)
- `ConfirmHigherTF(symbol, signalDirection string, mkt *market.Market) bool`
- Menggunakan `GetHigherTFCloses(symbol, 6)` — agregasi setiap 6 candle menjadi 1 candle HTF
- BUY valid hanya jika EMA9 > EMA21 di HTF; SELL valid jika EMA9 < EMA21
- Jika data tidak cukup (< 22 candle HTF): return true (jangan blokir trade)
- Diterapkan di `Tick()` untuk semua strategy kecuali Scalping

### D. Breakeven + Trailing Stop (`internal/bot/trade_manager.go`)
- `UpdateTrailingStop(trade, currentPrice, symbol)` — dipanggil setiap tick
- Breakeven: profit ≥ 20 pip → SL dipindah ke entry price
- Trailing: profit ≥ 30 pip → SL trailing dengan jarak 15 pip dari harga saat ini
- Trade struct diperluas: `SLPrice`, `TPPrice`, `BreakevenSet`, `TrailingActive`, `IsDryRun`

### E. Order Validation (`internal/mt5/order.go`)
- `ValidateOrder(symbol, volume, marginRequired float64, acc *AccountInfo) error`
- Cek 1: `margin_required < free_margin × 0.8`
- Cek 2: `volume >= 0.01` (minimal lot)
- Cek 3: volume adalah kelipatan `0.01` (lot step)
- Cek 4: tolak order di jam **13:30–14:30 UTC** (news blackout)

## Log & Monitoring (Bagian 3)

### A. Enhanced Logging (`internal/logger/logger.go`)
- `OrderFailed(botID, symbol, reason)` — log order yang ditolak beserta alasan detail
- `DrawdownSnapshot(currentEquity, peakEquity, drawdownPct)` — log setiap 5 menit via tickMsg
- `DailyPL(todayProfit, todayLoss, winRate)` — log otomatis saat pergantian hari (midnight)
- Semua tersimpan di `finex-bot.log` dengan format konsisten `KIND | timestamp | detail`

### B. Paper Trading Mode (`main.go`)
- Flag `--dry-run`: `./finex-bot --dry-run`
- Semua order ditandai `IsDryRun: true` pada Trade struct
- Slippage acak **±1 pip** diterapkan ke entry price untuk realisme simulasi
- Session log dimulai dengan mode `"DRY-RUN"` (bukan `"DEMO"`)
- `DryRun bool` di-wire ke setiap bot dari `initialModel(dryRun bool)`

## Integrasi & Perbaikan Dasar (Bagian 4)

### A. Real Price Feed (`internal/mt5/pricefeed.go`)
- Struct `PriceFeed` dengan `sync.RWMutex`, map `latest` per simbol
- `Start(client, mkt, symbols)` — goroutine background, non-blocking
- Subscribe ke MT5 menggunakan `CmdSubscribeTick (0x0100)` per simbol (null-terminated body)
- Baca paket `CmdTickData (0x0101)` → decode `parseTickPacket()` → update `mkt.UpdatePrice()`
- Jika server tidak support command (fallback): berhenti gracefully, market simulator tetap jalan
- `market.UpdatePrice(symbol, price)` ditambahkan ke `Market` sebagai titik inject data live
- `Stop()`, `IsRunning()`, `GetLatest(symbol)` tersedia untuk manajemen lifecycle

### B. News Time Filter (`internal/utils/news.go`)
- `IsNewsTime() bool` — cek waktu UTC saat ini terhadap semua event berdampak tinggi
- `ActiveNewsName() string` — nama event yang sedang aktif (untuk ditampilkan di UI)
- Tiga jenis event ter-cover:
  * **NFP** (Non-Farm Payroll): Jumat pertama tiap bulan, 13:30 UTC — dihitung dinamis
  * **FOMC Rate Decision**: 8x/tahun, 19:00 UTC — hardcoded 2025 & 2026
  * **US CPI**: bulanan, 13:30 UTC — hardcoded 2025 & 2026
- Blackout window: **±15 menit** dari setiap event (total 30 menit per event)
- Diintegrasikan ke `bot.Tick()`: semua entry dilewati saat `IsNewsTime()` = true

## Optimalisasi Lanjutan (Bagian 5)

### A. Smart Money Concepts — Order Block + FVG (`internal/strategy/smart_money.go`)
- `Analyze(candles, direction) SmartMoneyResult` — satu panggilan untuk semua analisis SMC
- **Order Block**:
  * BUY OB: candle bullish → candle bearish berikutnya (demand zone)
  * SELL OB: candle bearish → candle bullish berikutnya (supply zone)
- **Fair Value Gap (FVG)**:
  * Upward FVG: `Low[candle+2] > High[candle]` (gap permintaan)
  * Downward FVG: `High[candle+2] < Low[candle]` (gap penawaran)
- Scan 5 candle terakhir (`lookbackCandles = 5`)
- Weight: 0.30 jika FVG terdeteksi (konfirmasi lebih kuat)
- Diintegrasikan ke `openTrade()`: entry hanya dilakukan jika `smResult.Confirmed = true` (non-Scalping)

### B. Adaptive TP — RSI Divergence Monitor (`internal/bot/trade_manager.go`)
- `MonitorDivergence(highs, lows, closes, direction, ticksSinceOpen) bool`
- Scan hanya setiap 10 tick (`ticksSinceOpen % 10 == 0`) untuk hemat CPU
- **Hidden bullish divergence** (price LL + RSI HL) → exit BUY lebih awal
- **Hidden bearish divergence** (price HH + RSI LH) → exit SELL lebih awal
- `Trade.TicksSinceOpen` diinkremen setiap tick oleh `UpdateTrailingStop`
- `checkCloseCondition` diperluas dengan `highs, lows []float64` parameter
- Dipanggil di `checkCloseCondition` setelah trailing stop update, sebelum SL/TP check

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
