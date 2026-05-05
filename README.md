# Finex CLI — MT5 Trading Bot

Terminal UI trading bot untuk MetaTrader 5, dirancang khusus untuk broker **FinexBisnisSolusi**. Dibangun dengan Go + Bubble Tea.

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)
![MT5](https://img.shields.io/badge/MT5-Demo%2FLive-orange)
![Tests](https://img.shields.io/badge/tests-89%20passing-brightgreen)
![License](https://img.shields.io/badge/license-MIT-blue)

---

## Fitur Utama

| # | Fitur | Keterangan |
|---|---|---|
| A | Live indicator dashboard | RSI gauge, Bollinger %B, sinyal komposit per pasang, update setiap detik |
| B | 4 strategi trading | Scalping, Swing, Trend Following, Mean Reversion |
| C | 8 pasang forex | EURUSD, GBPUSD, USDJPY, AUDUSD, USDCAD, USDCHF, EURGBP, EURJPY |
| D | Multi-bot management | Buat, edit, start/stop, hapus bot lewat TUI; config disimpan di `bots.json` |
| E | Koneksi MT5 | TLS + SRP-6a authentication ke server MetaQuotes |
| F | Structured logging | Semua trade, event bot, dan error MT5 dicatat ke `finex-bot.log` |
| G | Risk management | Daily loss limit (5%), max drawdown (10%), dynamic lot sizing, cooldown |
| H | Correlation-aware sizing | Posisi berkorelasi tinggi otomatis dikurangi risikonya 30% |
| I | Market Regime Detection | ADX + ATR + BB mendeteksi Trending / Ranging / Volatile |
| J | Performance Dashboard | Win rate, Profit Factor, Sharpe Ratio, Max Drawdown (Tab 6) |
| K | Trading Session Filter | Hanya buka posisi saat sesi London (07–16 UTC) atau New York (13–22 UTC) |
| L | GA Optimizer | Genetic Algorithm otomatis cari parameter RSI/EMA/BB terbaik per simbol |
| M | Telegram Bot Profesional | Inline keyboard, edit-in-place, toggle bot per-tombol, symbol picker |

---

## Instalasi

### Prasyarat

- **Go 1.21+** — [download](https://go.dev/dl/)
- **Akun MetaTrader 5** (demo atau live) dari [Finex](https://finexindo.co.id)
- Terminal dengan dukungan warna 256-bit (iTerm2, Windows Terminal, GNOME Terminal, dll.)

### Clone & Build

```bash
git clone https://github.com/your-username/finex-cli.git
cd finex-cli

# Salin template environment variable
cp .env.example .env

# Edit .env dengan kredensial MT5 kamu
nano .env

# Build
go build -o finex-bot .

# Jalankan
./finex-bot
```

### Build tanpa clone (Go install)

```bash
go install github.com/your-username/finex-cli@latest
finex-cli
```

---

## Konfigurasi Environment Variable

Buat file `.env` di root project (sudah ada contohnya di `.env.example`):

```bash
cp .env.example .env
```

Lalu isi dengan data akun MT5 kamu:

```env
# MT5 Connection
FINEX_COMPANY=FinexBisnisSolusi
FINEX_LOGIN=your_login_number
FINEX_SERVER=FinexBisnisSolusi-Demo
FINEX_HOST=prod-mt5-demo1.fnx.xmt.mx:443
FINEX_PASSWORD=your_password

# Telegram Bot (opsional)
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
```

| Variable | Wajib | Keterangan |
|---|---|---|
| `FINEX_LOGIN` | Ya | Nomor akun MT5 (angka) |
| `FINEX_PASSWORD` | Ya | Password akun MT5 |
| `FINEX_SERVER` | Ya | Nama server MT5 (lihat di platform) |
| `FINEX_HOST` | Ya | `hostname:port` server MT5 |
| `FINEX_COMPANY` | Ya | Nama company broker (untuk verifikasi koneksi) |
| `TELEGRAM_BOT_TOKEN` | Tidak | Token dari @BotFather — aktifkan remote control via Telegram |
| `TELEGRAM_CHAT_ID` | Tidak | Chat ID kamu dari @userinfobot — untuk keamanan |

> **Penting:** File `.env` sudah masuk `.gitignore`. JANGAN pernah commit file ini ke repositori publik.

### Prioritas Pembacaan Konfigurasi

App membaca konfigurasi dengan urutan prioritas berikut:
1. **Environment variable** yang sudah diset di sistem (tertinggi — override `.env`)
2. **File `.env`** di direktori project
3. **Default value** yang sudah hardcode di kode (terendah)

### Beralih ke Akun Live

```env
FINEX_LOGIN=<nomor_akun_live>
FINEX_PASSWORD=<password_live>
FINEX_SERVER=FinexBisnisSolusi-Live
FINEX_HOST=<host_live>:443
```

Atau gunakan tombol `r` di dalam TUI pada tab **Settings** untuk toggle Demo ↔ Real.

> **Catatan:** Eksekusi order live belum diimplementasikan. Bot saat ini autentikasi ke MT5 dan menjalankan strategi dalam mode simulasi. Eksekusi order live direncanakan untuk rilis berikutnya.

---

## Cara Penggunaan TUI

Setelah menjalankan `./finex-bot`, kamu akan melihat antarmuka berikut:

```
 FINEX BOT  Demo  EURUSD ▼ 1.08417           Floating P&L: +0.00
─────────────────────────────────────────────────────────────────
 [1] DASHBOARD  [2] MARKETS  [3] BOTS  [4] TRADES  [5] SETTINGS  [6] METRICS
═════════════════════════════════════════════════════════════════
 ...konten tab...
─────────────────────────────────────────────────────────────────
  1-6  Jump   Tab  Next   ⇧Tab  Prev   q  Quit
```

### Navigasi

| Tombol | Aksi |
|---|---|
| `1` – `6` | Pindah langsung ke tab |
| `Tab` | Tab berikutnya |
| `Shift+Tab` | Tab sebelumnya |
| `↑` / `↓` | Pilih item dalam daftar |
| `q` atau `Ctrl+C` | Keluar |

### Manajemen Bot (Tab 3 — Bots)

| Tombol | Aksi |
|---|---|
| `n` | Buat bot baru |
| `e` | Edit bot yang dipilih |
| `d` | Hapus bot yang dipilih |
| `s` | Start / Stop bot yang dipilih |
| `↑` / `↓` | Pilih bot |

### Membuat Bot Baru

1. Tekan `3` untuk buka tab **Bots**
2. Tekan `n` → form pembuatan bot muncul
3. Isi:
   - **Name** — nama bot (bebas)
   - **Symbol** — pilih dari 8 pasang yang tersedia
   - **Strategy** — Scalping / Swing / Trend / Mean Reversion
   - **Risk %** — persentase risiko per trade (misal: `1.5`)
   - **Stop Loss (pips)** — jarak stop loss dalam pips
   - **Take Profit (pips)** — jarak take profit dalam pips
   - **Max Daily Loss %** — batas kerugian harian
   - **Max Drawdown %** — batas drawdown dari peak equity
4. Tekan `Enter` untuk simpan, `Esc` untuk batal
5. Kembali ke tab Bots → pilih bot → tekan `s` untuk start

---

## Penjelasan Tab

### [1] Dashboard
- **4 KPI tiles**: Balance, Equity, Session P&L, Win Rate + jumlah bot aktif
- **Tabel bot ringkas**: status, simbol, strategi, P&L, win rate per bot
- **Market Prices**: harga live 8 pasang dalam 2 kolom + arah (▲/▼) dan perubahan %
- **Recent Trades**: 5 trade terakhir lintas semua bot

### [2] Markets
- Sinyal komposit per pasang: LONG ↑ / SHORT ↓ / WAIT –
- RSI(7) gauge bar visual
- Bollinger %B posisi
- Spread dan ATR(14)

### [3] Bots
- Tabel semua bot (1 baris per bot)
- Detail card bot yang dipilih: strategi, risk, TP/SL, drawdown, trade aktif

### [4] Trades
- Riwayat semua trade yang ditutup

### [5] Settings
- Toggle Demo / Real (tombol `r`)
- Status koneksi MT5

### [6] Metrics (Performance Dashboard)
- Win Rate agregat
- Profit Factor
- Sharpe Ratio (simplified)
- Max Drawdown %
- Top pasang berdasarkan P&L
- Strategi terbaik berdasarkan profit factor
- Risk Meter: drawdown saat ini vs batas maksimal

---

## Strategi Trading

| Strategi | Indikator | Entry BUY | Entry SELL |
|---|---|---|---|
| **Scalping** | RSI(7) | RSI < 38 | RSI > 62 |
| **Swing Trading** | BB(20, 2σ) | Price ≤ lower band | Price ≥ upper band |
| **Trend Following** | EMA(9) × EMA(21) | Golden cross | Death cross |
| **Mean Reversion** | RSI(14) + BB(20, 1.5σ) | RSI < 35 atau price ≤ lower | RSI > 65 atau price ≥ upper |

Semua strategi menggunakan konfirmasi **Smart Money Concepts (SMC)**:
- **Order Block** — candle bullish diikuti bearish (demand zone untuk BUY)
- **Fair Value Gap (FVG)** — gap imbalance antar 3 candle berurutan (+30% bobot sinyal)

---

## Risk Management

### Dynamic Lot Sizing
```
lot = (equity × risk%) / (SL_pips × pip_value)
```
Dibulatkan ke kelipatan lot step (0.01), minimum `MinLot`.

### RiskLimits (Per Bot)
| Parameter | Nilai Default | Keterangan |
|---|---|---|
| Max Daily Loss | 5% equity | Bot berhenti jika rugi harian > 5% |
| Max Drawdown | 10% dari peak | Bot berhenti jika drawdown > 10% dari equity tertinggi |

### Dynamic Risk Profile (Rolling 20 Trade)
| Win Rate | Risk per Trade |
|---|---|
| > 60% | 2.0% |
| 40–60% | 1.5% (default) |
| < 40% | 1.0% |

- **3 consecutive losses** → risk dipotong 50% untuk trade berikutnya
- **5 consecutive losses** → cooldown 1 jam (trading halt)
- **Win trade** → reset consecutive loss ke 0

### Correlation-Aware Sizing
| Kondisi | Tindakan |
|---|---|
| Korelasi ≥ 0.7, posisi searah | Risk dikurangi 30% |
| Korelasi ≥ 0.7, posisi berlawanan | Entry diblokir |
| Total exposed correlated risk > 5% equity | Entry diblokir |

### Market Regime Detection
| Regime | Kriteria | Pengaruh |
|---|---|---|
| **TRENDING** | ADX > 25 + slope EMA ≠ 0 | Trend + Swing strategies diprioritaskan |
| **RANGING** | ADX < 20 + BB width < 1% | Mean Reversion + Scalping diprioritaskan |
| **VOLATILE** | ATR(14) > 1.5× mean ATR(50) | Risk semua strategi dikurangi 50% |

### Trading Session Filter
Bot hanya membuka posisi baru dalam sesi aktif:
- **London**: 07:00–16:00 UTC
- **New York**: 13:00–22:00 UTC
- **Asia** (22:00–07:00 UTC): tidak ada entry baru

---

## Telegram Bot

Finex CLI bisa dikontrol dari mana saja via Telegram dengan tampilan profesional — inline keyboard, edit pesan in-place (seperti bot Trojan), dan toggle per-bot langsung dari tombol.

### Setup

1. Buka Telegram → cari **@BotFather** → `/newbot` → salin token ke `TELEGRAM_BOT_TOKEN`
2. Buka **@userinfobot** → salin "Id" kamu ke `TELEGRAM_CHAT_ID`
3. Isi `.env` lalu restart app — dashboard lengkap dengan tombol akan muncul otomatis

### Tampilan Dashboard

```
🤖 FINEX TRADING BOT
━━━━━━━━━━━━━━━━━━━━━━
📡 MT5: ❌ Offline  |  🔧 Mode: DEMO
🌍 Sesi: London
━━━━━━━━━━━━━━━━━━━━━━
💰 Balance   $ 10000.00
💎 Equity    $ 10000.00
━━━━━━━━━━━━━━━━━━━━━━
🟢 Bot Aktif  2 / 3
━━━━━━━━━━━━━━━━━━━━━━
Pilih aksi:

[▶️ Start All]  [⏹ Stop All]
[📊 Status]  [💰 Balance]  [📈 Trades]
[🤖 Kelola Bot]  [⚙️ Optimize]
[🔄 Refresh]
```

### Perintah Teks (Alternatif Tombol)

| Perintah | Fungsi |
|---|---|
| `/help` atau `/menu` | Tampilkan dashboard utama dengan tombol |
| `/status` | Status MT5, mode, sesi, saldo, drawdown |
| `/bots` | Daftar bot + tombol toggle per bot |
| `/startbot` | Mulai semua bot |
| `/stopbot` | Hentikan semua bot |
| `/trades` | Posisi terbuka + unrealized P&L |
| `/balance` | Saldo & equity akun |
| `/optimize` | Buka symbol picker untuk GA optimizer |

### Kelola Bot via Tombol

Tap **🤖 Kelola Bot** → muncul daftar bot dengan tombol toggle per bot:

```
🤖 KELOLA BOT
━━━━━━━━━━━━━━━━━━━━━━
1. 🟢 EUR Scalper
   📌 EURUSD  ·  Scalping
   📊 Status  : Aktif
   🟢 P&L    : $+45.20  ·  W:12 / L:5

[🟢 EUR Scalper · ⏹ Stop]
[🔴 GBP Trend   · ▶️ Start]
[▶️ Start All]  [⏹ Stop All]
[🔄 Refresh]  [🏠 Menu]
```

Tap tombol bot → status toggle langsung, pesan diperbarui in-place.

### Optimizer via Tombol

Tap **⚙️ Optimize** → pilih simbol dari keyboard:

```
[EURUSD]  [GBPUSD]  [USDJPY]
[AUDUSD]  [USDCAD]  [USDCHF]
[EURGBP]  [EURJPY]
[🏠 Menu]
```

Tap simbol → GA optimizer berjalan 4 strategi × 10 generasi × 20 kromosom → hasil dikirim otomatis ke chat.

### Notifikasi Otomatis

Bot mengirim alert tanpa polling manual:

| Event | Trigger |
|---|---|
| ⚠️ **Drawdown Alert** | Equity turun ≥5% dari peak (max 1x/jam) |
| 📊 **Laporan Harian** | Setiap tengah malam — total P&L, win rate, trade count |

> **Keamanan:** Semua perintah dan tombol hanya merespons dari `TELEGRAM_CHAT_ID` yang terdaftar. Permintaan dari chat lain langsung ditolak.

---

## GA Optimizer

Jalankan Genetic Algorithm untuk mencari parameter indikator terbaik per simbol:

```bash
# Via Telegram: tap ⚙️ Optimize → pilih simbol
# Via TUI: belum tersedia (roadmap)
```

Optimizer menjalankan **4 strategi sekaligus** untuk setiap simbol:
- Scalping, Trend Following, Swing Trading, Mean Reversion

Parameter yang dioptimalkan:
- RSI period + threshold buy/sell
- EMA fast + slow period
- Bollinger Bands period + multiplier

Hasil disimpan ke `optimized_params.json` dan diterapkan otomatis ke semua bot saat app restart.

---

## Struktur Proyek

```
finex-cli/
├── main.go                          # TUI entry point (Bubble Tea model + semua view)
├── .env.example                     # Template environment variable
├── .gitignore
├── LICENSE
├── go.mod / go.sum
└── internal/
    ├── account/
    │   └── account.go               # Tipe akun Demo / Real
    ├── bot/
    │   ├── bot.go                   # Logika bot, lifecycle trade, default bots
    │   ├── risk.go                  # RiskLimits, RiskProfile, CalculateLotSize
    │   ├── risk_test.go
    │   └── trade_manager.go         # Manajemen posisi terbuka
    ├── config/
    │   └── config.go                # Simpan/load bots.json
    ├── indicator/
    │   ├── indicator.go             # RSI, EMA, SMA, Bollinger Bands, ATR, ADX, cache
    │   └── indicator_test.go
    ├── journal/
    │   └── journal.go               # Trade journal (JSONL) + equity curve HTML
    ├── logger/
    │   └── logger.go                # Structured file logger
    ├── market/
    │   ├── market.go                # Simulasi forex market + candle history
    │   ├── regime.go                # Market regime detection (ADX + ATR + BB)
    │   └── regime_test.go
    ├── mt5/
    │   ├── client.go                # TLS connection + SRP-6a auth + heartbeat
    │   ├── account.go               # Binary account info parser
    │   ├── heartbeat.go             # Keepalive loop
    │   ├── order.go                 # Order submission (placeholder)
    │   ├── pricefeed.go             # Live price streaming
    │   ├── proto.go                 # MT5 binary packet encoding/decoding
    │   └── srp6a.go                 # SRP-6a (RFC 5054, Group 14 / SHA-256)
    ├── optimizer/
    │   ├── genetic.go               # Genetic Algorithm (populasi, seleksi, mutasi)
    │   └── apply.go                 # ApplyToBots: baca optimized_params.json → set ke bot
    ├── risk/
    │   ├── correlation.go           # Correlation matrix + exposure cap
    │   └── correlation_test.go
    ├── strategy/
    │   ├── smart_money.go           # Order Block + FVG detection
    │   └── smart_money_test.go
    ├── telegram/
    │   └── bot.go                   # Professional Telegram bot: inline keyboard, edit-in-place
    └── utils/
        ├── news.go                  # High-impact news blackout (FOMC, NFP, CPI)
        ├── news_test.go
        └── session.go               # Trading session filter (London / New York / Asia)
```

---

## Menjalankan Test

```bash
# Semua paket
go test ./internal/... -v

# Satu paket saja
go test ./internal/risk/... -v

# Dengan coverage
go test ./internal/... -cover
```

Output yang diharapkan: **89 tests, semua PASS**.

---

## Troubleshooting

| Masalah | Solusi |
|---|---|
| `Loading Finex Trading Bot...` terus-menerus | Perkecil terminal atau tunggu inisialisasi selesai |
| Koneksi MT5 gagal | Periksa `FINEX_HOST`, `FINEX_LOGIN`, `FINEX_PASSWORD` di `.env` |
| TUI berantakan / tidak aligned | Pastikan terminal width minimal 80 kolom |
| Bot tidak bisa start | Cek log di `finex-bot.log` untuk detail error |
| `bots.json` tidak tersimpan | Pastikan direktori project memiliki permission write |
| Telegram tidak merespons | Pastikan `TELEGRAM_BOT_TOKEN` dan `TELEGRAM_CHAT_ID` sudah diisi benar di `.env` |
| Optimizer tidak tersimpan | Pastikan direktori punya akses write; cek `optimized_params.json` |

---

## Kontribusi

1. Fork repositori ini
2. Buat branch baru: `git checkout -b fitur/nama-fitur`
3. Commit perubahan: `git commit -m "feat: tambah fitur X"`
4. Push ke branch: `git push origin fitur/nama-fitur`
5. Buka Pull Request

Pastikan semua test lulus sebelum membuka PR:
```bash
go test ./internal/... && go vet ./...
```

---

## Lisensi

MIT — lihat [LICENSE](LICENSE) untuk detail lengkap.
