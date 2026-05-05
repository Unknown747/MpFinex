package main

import (
        "fmt"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/charmbracelet/bubbles/textinput"
        tea "github.com/charmbracelet/bubbletea"
        "github.com/charmbracelet/lipgloss"

        "math"
        "sort"

        "github.com/finex/finex-cli/internal/account"
        botpkg "github.com/finex/finex-cli/internal/bot"
        "github.com/finex/finex-cli/internal/config"
        "github.com/finex/finex-cli/internal/indicator"
        "github.com/finex/finex-cli/internal/journal"
        "github.com/finex/finex-cli/internal/logger"
        "github.com/finex/finex-cli/internal/market"
        "github.com/finex/finex-cli/internal/mt5"
        "github.com/finex/finex-cli/internal/optimizer"
        "github.com/finex/finex-cli/internal/risk"
        "github.com/finex/finex-cli/internal/telegram"
)

// ─── Tick ───────────────────────────────────────────────────────────────────

type tickMsg time.Time

func tickCmd() tea.Cmd {
        return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
                return tickMsg(t)
        })
}

// indicatorSem is a goroutine pool semaphore that limits concurrent indicator
// computations to 4 goroutines regardless of how many symbols are active.
var indicatorSem = make(chan struct{}, 4)

// ─── MT5 Connection ──────────────────────────────────────────────────────────

type mt5ConnMsg mt5.Status
type mt5AccInfoMsg struct{ acc *mt5.AccountInfo }
type mt5RetryMsg struct{ attempt int } // fired by backoff timer
type heartbeatShutdownMsg struct{}     // fired when heartbeat gives up reconnecting
type telegramCmdMsg telegram.Command  // fired when a Telegram command arrives

// waitTelegramCmd blocks until a command arrives from the Telegram goroutine,
// then delivers it to the TUI event loop as telegramCmdMsg.
func waitTelegramCmd(ch <-chan telegram.Command) tea.Cmd {
        return func() tea.Msg {
                cmd := <-ch
                return telegramCmdMsg(cmd)
        }
}

// watchHeartbeatShutdown menunggu sinyal shutdown dari heartbeat goroutine,
// lalu mengirim heartbeatShutdownMsg ke TUI event loop.
func watchHeartbeatShutdown(ch chan struct{}) tea.Cmd {
        return func() tea.Msg {
                <-ch
                return heartbeatShutdownMsg{}
        }
}

// connectMT5 runs the full MT5 handshake in a background goroutine.
func connectMT5(c *mt5.Client) tea.Cmd {
        return func() tea.Msg {
                c.Connect()
                return mt5ConnMsg(c.Status)
        }
}

// waitMT5AccInfo delivers account info to the TUI right after Connect() succeeds.
func waitMT5AccInfo(c *mt5.Client) tea.Cmd {
        return func() tea.Msg {
                if c.Account != nil {
                        return mt5AccInfoMsg{acc: c.Account}
                }
                return nil
        }
}

// scheduleReconnect returns a Cmd that waits for the backoff delay then fires
// mt5RetryMsg. Exponential backoff: 5s → 10s → 20s → … → 300s cap.
func scheduleReconnect(attempt int) tea.Cmd {
        delay := mt5BackoffDelay(attempt)
        return func() tea.Msg {
                time.Sleep(delay)
                return mt5RetryMsg{attempt: attempt}
        }
}

// mt5BackoffDelay computes the wait duration for the given attempt index.
func mt5BackoffDelay(attempt int) time.Duration {
        const base = 5 * time.Second
        const cap = 5 * time.Minute
        d := base
        for i := 0; i < attempt; i++ {
                d *= 2
                if d > cap {
                        d = cap
                        break
                }
        }
        // Add ±10% jitter using current nanosecond
        jitter := time.Duration(time.Now().UnixNano()%int64(d/10)) - d/20
        d += jitter
        if d < time.Second {
                d = time.Second
        }
        return d
}

// ─── Tabs ────────────────────────────────────────────────────────────────────

type Tab int

const (
        TabDashboard Tab = iota
        TabMarkets
        TabBots
        TabTrades
        TabSettings
        TabMetrics
)

var tabNames = []string{"Dashboard", "Markets", "Bots", "Trades", "Settings", "Metrics"}

// ─── Mode for bot form ────────────────────────────────────────────────────────

type ViewMode int

const (
        ViewList ViewMode = iota
        ViewBotForm
        ViewBotDetail
        ViewConfirmSwitch
)

// ─── Styles ─────────────────────────────────────────────────────────────────────────────

var (
        // Palette — deep navy terminal trading theme
        colorBg       = lipgloss.Color("#080d14")
        colorSurface  = lipgloss.Color("#0d1421")
        colorSurface2 = lipgloss.Color("#111c2d")
        colorBorder   = lipgloss.Color("#1e3a5c")
        colorPrimary  = lipgloss.Color("#38bdf8")
        colorGreen    = lipgloss.Color("#22c55e")
        colorRed      = lipgloss.Color("#f43f5e")
        colorYellow   = lipgloss.Color("#eab308")
        colorMuted    = lipgloss.Color("#475569")
        colorWhite    = lipgloss.Color("#f1f5f9")
        colorOrange   = lipgloss.Color("#fb923c")
        colorViolet   = lipgloss.Color("#a78bfa")
        colorDim      = lipgloss.Color("#1e293b")

        baseStyle = lipgloss.NewStyle().
                        Background(colorBg).
                        Foreground(colorWhite)

        headerStyle = lipgloss.NewStyle().
                        Background(colorSurface).
                        Foreground(colorPrimary).
                        Bold(true).
                        Padding(0, 2)

        tabActiveStyle = lipgloss.NewStyle().
                        Background(colorPrimary).
                        Foreground(colorBg).
                        Bold(true).
                        Padding(0, 2)

        tabInactiveStyle = lipgloss.NewStyle().
                                Background(colorSurface).
                                Foreground(colorMuted).
                                Padding(0, 2)

        demoTagStyle = lipgloss.NewStyle().
                        Background(colorYellow).
                        Foreground(colorBg).
                        Bold(true).
                        Padding(0, 1)

        realTagStyle = lipgloss.NewStyle().
                        Background(colorGreen).
                        Foreground(colorBg).
                        Bold(true).
                        Padding(0, 1)

        cardStyle = lipgloss.NewStyle().
                        Border(lipgloss.NormalBorder()).
                        BorderForeground(colorBorder).
                        Background(colorSurface).
                        Padding(0, 2)

        cardHiStyle = lipgloss.NewStyle().
                        Border(lipgloss.NormalBorder()).
                        BorderForeground(colorPrimary).
                        Background(colorSurface2).
                        Padding(0, 2)

        titleStyle  = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
        mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
        dimStyle    = lipgloss.NewStyle().Foreground(colorDim)
        greenStyle  = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
        redStyle    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
        yellowStyle = lipgloss.NewStyle().Foreground(colorYellow)
        violetStyle = lipgloss.NewStyle().Foreground(colorViolet)

        inputStyle = lipgloss.NewStyle().
                        Border(lipgloss.NormalBorder()).
                        BorderForeground(colorBorder).
                        Padding(0, 1)

        focusedInputStyle = lipgloss.NewStyle().
                                Border(lipgloss.NormalBorder()).
                                BorderForeground(colorPrimary).
                                Padding(0, 1)

        helpStyle = lipgloss.NewStyle().
                        Foreground(colorMuted)
)

// winBar renders a horizontal progress bar for win rate (0-100).
func winBar(pct float64, width int) string {
        if width < 2 {
                width = 2
        }
        filled := int(pct / 100.0 * float64(width))
        if filled > width {
                filled = width
        }
        var col lipgloss.Color
        switch {
        case pct >= 55:
                col = colorGreen
        case pct >= 40:
                col = colorYellow
        default:
                col = colorRed
        }
        bar := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("█", filled)) +
                lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("░", width-filled))
        return bar
}

// kpiTile renders a small KPI box with a label on top and a value below.
func kpiTile(label, value string, w int, valSty lipgloss.Style) string {
        inner := mutedStyle.Render(label) + "\n" + valSty.Render(value)
        return lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                BorderForeground(colorBorder).
                Background(colorSurface).
                Padding(0, 2).
                Width(w).
                Render(inner)
}

// keyHint returns a styled [KEY] label + description string for the help bar.
func keyHint(key, desc string) string {
        k := lipgloss.NewStyle().
                Foreground(colorBg).
                Background(lipgloss.Color("#64748b")).
                Bold(true).
                Padding(0, 1).
                Render(key)
        return k + " " + mutedStyle.Render(desc)
}

func pnlStyle(v float64) lipgloss.Style {
        if v > 0 {
                return greenStyle
        } else if v < 0 {
                return redStyle
        }
        return mutedStyle
}

// ─── Model ────────────────────────────────────────────────────────────────────

type Model struct {
        width  int
        height int

        activeTab Tab
        viewMode  ViewMode

        demoAccount *account.Account
        realAccount *account.Account
        useReal     bool

        mkt  *market.Market
        bots []*botpkg.Bot

        selectedBot int

        // bot form
        botFormInputs  []textinput.Model
        botFormFocused int
        botFormSymIdx  int
        botFormStrIdx  int
        botFormEditing bool
        botFormBotID   int

        tickCount int

        // MT5 connection
        mt5Client  *mt5.Client
        mt5Status  mt5.Status
        mt5Err     string
        mt5Account *mt5.AccountInfo // live account data when connected

        // Auto-reconnect state
        mt5RetryCount int       // total attempts so far
        mt5NextRetry  time.Time // when the next reconnect attempt fires

        // Heartbeat watchdog
        heartbeatStarted    bool
        heartbeatShutdownCh chan struct{}

        // Activity logger
        log *logger.Logger

        // Periodic logging state
        lastDrawdownLog time.Time // last time DrawdownSnapshot was logged
        lastDailyLog    time.Time // last date DailyPL was logged (check day change)
        peakEquity      float64   // peak equity seen this session (for drawdown calc)

        // Paper trading
        dryRun bool

        // Trade journal — records all closed trades and generates equity_chart.html
        journal *journal.Journal

        // Market regime detection — refreshed every hour for all active symbols
        regime           *market.RegimeDetector
        lastRegimeUpdate time.Time

        // Correlation-based position sizing
        corrMgr *risk.CorrelationManager

        // Metrics tab — cache timestamp (recomputed every 30 seconds)
        metricsUpdated time.Time

        // Telegram bot — nil if TELEGRAM_BOT_TOKEN not set
        tgBot *telegram.Bot
}

// logPath is where the activity log is written.
const logPath = "finex-bot.log"

func initialModel(dryRun bool) Model {
        dm := account.NewDemoAccount()
        rm := account.NewRealAccount()
        mkt := market.NewMarket()
        bots, _ := config.LoadBots()
        if len(bots) == 0 {
                bots = botpkg.DefaultBots()
        }
        // Wire dry-run flag to every bot
        for _, b := range bots {
                b.DryRun = dryRun
        }
        mt5Client := mt5.NewClient(mt5.ConfigFromEnv())

        // Open activity log (ignore error — bot runs fine without it).
        lg, _ := logger.New(logPath)

        mode := "DEMO"
        if dryRun {
                mode = "DRY-RUN"
        }
        if lg != nil {
                lg.SessionStart("1.0.0", mode)
        }

        // Open trade journal (best-effort; bot runs fine without it).
        jnl := journal.New("trade_journal.jsonl", dm.Equity)

        regime := market.NewRegimeDetector()
        corrMgr := risk.NewCorrelationManager()

        m := Model{
                activeTab:    TabDashboard,
                viewMode:     ViewList,
                demoAccount:  dm,
                realAccount:  rm,
                useReal:      false,
                mkt:          mkt,
                bots:         bots,
                selectedBot:  0,
                mt5Client:    mt5Client,
                mt5Status:    mt5.StatusDisconnected,
                log:          lg,
                lastDailyLog: time.Now(),
                peakEquity:   dm.Equity,
                dryRun:       dryRun,
                journal:      jnl,
                regime:       regime,
                corrMgr:      corrMgr,
        }

        // Wire trade callbacks so every open/close is logged automatically.
        m.wireBotLogCallbacks()
        // Wire correlation + regime callbacks to every bot.
        m.wireBotRiskCallbacks()

        // Start Telegram bot if token is configured (best-effort).
        tg, err := telegram.FromEnv()
        if err != nil {
                if lg != nil {
                        lg.MT5Error("telegram: " + err.Error())
                }
        } else if tg != nil {
                tg.Start()
                m.tgBot = tg
                tg.Send("🟢 <b>Finex CLI aktif.</b> Ketik /help untuk daftar perintah.")
        }

        m.initBotForm()
        return m
}

// preComputeIndicators pre-computes RSI, ATR, EMA, and Bollinger Bands for
// every symbol that has at least one active bot, using a goroutine pool
// (indicatorSem, max 4 workers) to bound CPU consumption.
//
// Results are stored in the shared indicator cache (5-second TTL).  Bot.Tick()
// reads the cache via GetCachedIndicator and only falls back to direct
// computation on a cache miss, which eliminates redundant work when multiple
// bots trade the same currency pair.
func (m *Model) preComputeIndicators() {
        // Collect unique symbols from bots that currently need market data.
        symSet := make(map[string]struct{})
        for _, b := range m.bots {
                if b.IsRunning || b.PendingLimit != nil || b.OpenTrade != nil {
                        symSet[b.Symbol] = struct{}{}
                }
        }
        if len(symSet) == 0 {
                return
        }

        var wg sync.WaitGroup
        for sym := range symSet {
                sym := sym
                wg.Add(1)
                indicatorSem <- struct{}{} // acquire slot (blocks when pool is full)
                go func() {
                        defer wg.Done()
                        defer func() { <-indicatorSem }()

                        closes := m.mkt.GetCloses(sym)
                        highs, lows := m.mkt.GetHighLows(sym)

                        // RSI — Scalping uses period 7, MeanReversion uses period 14
                        indicator.SetCachedIndicator("RSI7_"+sym, indicator.RSI(closes, 7))
                        indicator.SetCachedIndicator("RSI14_"+sym, indicator.RSI(closes, 14))

                        // ATR(14) — used for lot sizing and SL/TP distance
                        indicator.SetCachedIndicator("ATR14_"+sym, indicator.ATR(highs, lows, closes, 14))

                        // Bollinger Bands — Swing (2σ) and MeanReversion (1.5σ)
                        _, bb20u, bb20l := indicator.BollingerBands(closes, 20, 2.0)
                        indicator.SetCachedIndicator("BB20u_"+sym, bb20u)
                        indicator.SetCachedIndicator("BB20l_"+sym, bb20l)
                        _, bb15u, bb15l := indicator.BollingerBands(closes, 20, 1.5)
                        indicator.SetCachedIndicator("BB15u_"+sym, bb15u)
                        indicator.SetCachedIndicator("BB15l_"+sym, bb15l)

                        // EMA(9/21) current + previous tick — TrendFollowing crossover
                        indicator.SetCachedIndicator("EMA9_"+sym, indicator.EMA(closes, 9))
                        indicator.SetCachedIndicator("EMA21_"+sym, indicator.EMA(closes, 21))
                        n := len(closes)
                        if n > 1 {
                                prev := closes[:n-1]
                                indicator.SetCachedIndicator("EMA9p_"+sym, indicator.EMA(prev, 9))
                                indicator.SetCachedIndicator("EMA21p_"+sym, indicator.EMA(prev, 21))
                        }
                }()
        }
        wg.Wait()
}

// wireBotLogCallbacks attaches OnTradeOpen / OnTradeClose to every bot.
// Safe to call with a nil logger — callbacks just become no-ops.
func (m *Model) wireBotLogCallbacks() {
        for _, b := range m.bots {
                b := b // capture
                b.OnTradeOpen = func(ev botpkg.TradeEvent) {
                        if m.log != nil {
                                m.log.TradeOpen(
                                        ev.Bot.ID, ev.Bot.Name,
                                        ev.Trade.Symbol, string(ev.Trade.Side),
                                        ev.Trade.ID, ev.Trade.EntryPrice, ev.Trade.Quantity,
                                )
                        }
                        if m.tgBot != nil {
                                sideEmoji := "🟢"
                                if ev.Trade.Side == botpkg.Sell {
                                        sideEmoji = "🔴"
                                }
                                m.tgBot.Send(fmt.Sprintf(
                                        "%s <b>TRADE BUKA</b>\n"+
                                                "Bot: %s\n"+
                                                "Pair: %s | %s\n"+
                                                "Entry: %.5f\n"+
                                                "Qty: %.2f lot",
                                        sideEmoji,
                                        ev.Bot.Name,
                                        ev.Trade.Symbol, string(ev.Trade.Side),
                                        ev.Trade.EntryPrice,
                                        ev.Trade.Quantity,
                                ))
                        }
                }
                b.OnTradeClose = func(ev botpkg.TradeEvent) {
                        if m.log != nil {
                                m.log.TradeClose(
                                        ev.Bot.ID, ev.Bot.Name,
                                        ev.Trade.Symbol, string(ev.Trade.Side),
                                        ev.Trade.ID,
                                        ev.Trade.EntryPrice, ev.Trade.ExitPrice,
                                        ev.Trade.PnL, ev.Trade.Quantity,
                                        ev.Trade.OpenedAt, ev.Trade.ClosedAt,
                                )
                        }
                        if m.journal != nil {
                                t := ev.Trade
                                pip := botpkg.DefaultSymbolInfo[t.Symbol].PipSize
                                if pip == 0 {
                                        pip = 0.0001
                                }
                                profitPips := (t.ExitPrice - t.EntryPrice) / pip
                                if t.Side == botpkg.Sell {
                                        profitPips = -profitPips
                                }
                                riskPct := ev.Bot.RiskPct
                                if ev.Bot.Profile != nil {
                                        riskPct = ev.Bot.Profile.EffectiveRisk()
                                }
                                m.journal.Record(journal.TradeRecord{
                                        ID:             fmt.Sprintf("%s_%s_%d", t.Symbol, string(t.Side), t.OpenedAt.UnixNano()),
                                        Timestamp:      t.ClosedAt,
                                        Symbol:         t.Symbol,
                                        Strategy:       string(ev.Bot.Strategy),
                                        Direction:      string(t.Side),
                                        EntryPrice:     t.EntryPrice,
                                        ExitPrice:      t.ExitPrice,
                                        ProfitPips:     profitPips,
                                        ProfitUSD:      t.PnL,
                                        RiskPercent:    riskPct,
                                        ExitReason:     t.CloseReason,
                                        SlippagePips:   t.SlippagePips,
                                        HoldingMinutes: t.ClosedAt.Sub(t.OpenedAt).Minutes(),
                                        MAEPips:        t.MAEPips,
                                        MFEPips:        t.MFEPips,
                                })
                        }
                        if m.tgBot != nil {
                                t := ev.Trade
                                pip := botpkg.DefaultSymbolInfo[t.Symbol].PipSize
                                if pip == 0 {
                                        pip = 0.0001
                                }
                                profitPips := (t.ExitPrice - t.EntryPrice) / pip
                                if t.Side == botpkg.Sell {
                                        profitPips = -profitPips
                                }
                                pnlEmoji := "✅"
                                if t.PnL < 0 {
                                        pnlEmoji = "❌"
                                }
                                holding := telegram.FormatDuration(t.ClosedAt.Sub(t.OpenedAt))
                                m.tgBot.Send(fmt.Sprintf(
                                        "%s <b>TRADE TUTUP</b>\n"+
                                                "Bot: %s\n"+
                                                "Pair: %s | %s\n"+
                                                "Entry: %.5f → Exit: %.5f\n"+
                                                "P&amp;L: <b>%+.2f pip / $%+.2f</b>\n"+
                                                "Alasan: %s | Durasi: %s",
                                        pnlEmoji,
                                        ev.Bot.Name,
                                        t.Symbol, string(t.Side),
                                        t.EntryPrice, t.ExitPrice,
                                        profitPips, t.PnL,
                                        t.CloseReason, holding,
                                ))
                        }
                }
        }
}

// wireBotRiskCallbacks attaches CorrCheck and RegimeMult callbacks to every bot.
// CorrCheck snapshots all currently open positions from OTHER bots and asks the
// CorrelationManager whether the proposed trade is allowed and at what risk.
// RegimeMult returns the regime-based risk scaling factor for the given symbol.
func (m *Model) wireBotRiskCallbacks() {
        for _, b := range m.bots {
                b := b // capture for closure
                b.CorrCheck = func(symbol, direction string) (bool, float64) {
                        var openPositions []risk.OpenPosition
                        for _, other := range m.bots {
                                if other.ID == b.ID || other.OpenTrade == nil {
                                        continue
                                }
                                openPositions = append(openPositions, risk.OpenPosition{
                                        Symbol:    other.OpenTrade.Symbol,
                                        Direction: string(other.OpenTrade.Side),
                                        RiskPct:   other.RiskPct,
                                })
                        }
                        return m.corrMgr.CheckEntry(symbol, direction, openPositions, b.RiskPct)
                }
                b.RegimeMult = func(symbol, strategy string) float64 {
                        if m.regime == nil {
                                return 1.0
                        }
                        r := m.regime.Detect(symbol, m.mkt)
                        return market.RiskMultiplier(r)
                }
        }
}

func (m *Model) currentAccount() *account.Account {
        if m.useReal {
                return m.realAccount
        }
        return m.demoAccount
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
        cmds := []tea.Cmd{tickCmd(), tea.EnterAltScreen, connectMT5(m.mt5Client)}
        if m.tgBot != nil {
                cmds = append(cmds, waitTelegramCmd(m.tgBot.CmdChan()))
        }
        return tea.Batch(cmds...)
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
        switch msg := msg.(type) {

        case tea.WindowSizeMsg:
                m.width = msg.Width
                m.height = msg.Height

        case mt5ConnMsg:
                m.mt5Status = mt5.Status(msg)
                m.mt5Err = m.mt5Client.ErrMsg

                switch m.mt5Status {
                case mt5.StatusConnected:
                        // Success — reset retry counter and pull account info
                        m.mt5RetryCount = 0
                        m.mt5NextRetry = time.Time{}
                        if m.log != nil {
                                m.log.MT5Connecting(m.mt5Client.Config.Host, m.mt5Client.Config.Login)
                        }

                        // Mulai heartbeat watchdog (hanya sekali per sesi)
                        if !m.heartbeatStarted {
                                m.heartbeatStarted = true
                                m.heartbeatShutdownCh = make(chan struct{})
                                shutdownCh := m.heartbeatShutdownCh
                                bots := m.bots
                                mkt := m.mkt
                                lg := m.log
                                mt5.StartHeartbeat(m.mt5Client, 3*time.Second, func() {
                                        for _, b := range bots {
                                                p := mkt.GetPrice(b.Symbol)
                                                if p != nil {
                                                        b.CloseAllPositions(p.Price)
                                                }
                                                b.IsRunning = false
                                        }
                                        if lg != nil {
                                                lg.MT5Disconnected("heartbeat: permanent failure after 3 reconnect attempts")
                                        }
                                        close(shutdownCh)
                                })
                                return m, tea.Batch(waitMT5AccInfo(m.mt5Client), watchHeartbeatShutdown(m.heartbeatShutdownCh))
                        }
                        return m, waitMT5AccInfo(m.mt5Client)

                case mt5.StatusFailed:
                        // Schedule exponential-backoff reconnect
                        delay := mt5BackoffDelay(m.mt5RetryCount)
                        m.mt5NextRetry = time.Now().Add(delay)
                        if m.log != nil {
                                m.log.MT5Error(fmt.Sprintf("attempt=%d err=%s retry_in=%s",
                                        m.mt5RetryCount+1, m.mt5Err, delay.Round(time.Second)))
                        }
                        return m, scheduleReconnect(m.mt5RetryCount)
                }
                return m, nil

        case mt5RetryMsg:
                m.mt5RetryCount = msg.attempt + 1
                m.mt5NextRetry = time.Time{}
                if m.log != nil {
                        m.log.MT5Connecting(m.mt5Client.Config.Host, m.mt5Client.Config.Login)
                }
                return m, connectMT5(m.mt5Client)

        case heartbeatShutdownMsg:
                // Heartbeat menyerah setelah 3x reconnect gagal.
                // Semua posisi sudah ditutup oleh callback. Quit TUI dengan aman.
                if m.log != nil {
                        totalPnL := 0.0
                        totalTrades := 0
                        for _, b := range m.bots {
                                totalPnL += b.TotalPnL
                                totalTrades += b.TradeCount()
                        }
                        m.log.SessionEnd(totalPnL, totalTrades)
                        m.log.Close()
                }
                return m, tea.Quit

        case mt5AccInfoMsg:
                if msg.acc != nil {
                        m.mt5Account = msg.acc
                        if m.log != nil {
                                m.log.MT5Authenticated(msg.acc.Login, msg.acc.Balance, msg.acc.Currency)
                        }
                        // Sync live balance into whichever account is active
                        acc := m.currentAccount()
                        acc.Balance = msg.acc.Balance
                        acc.Equity = msg.acc.Equity
                        acc.Name = msg.acc.Name
                        acc.Currency = msg.acc.Currency
                }
                return m, nil

        case tickMsg:
                m.tickCount++
                m.mkt.Tick()

                // Pre-compute indicators for all active symbols using goroutine pool
                // (max 4 concurrent) so bots on the same pair share results.
                m.preComputeIndicators()

                // Update market regime every hour for all known symbols.
                if m.regime != nil && time.Since(m.lastRegimeUpdate) >= time.Hour {
                        m.lastRegimeUpdate = time.Now()
                        syms := []string{"EURUSD", "GBPUSD", "USDJPY", "AUDUSD",
                                "USDCAD", "USDCHF", "EURGBP", "EURJPY"}
                        for _, sym := range syms {
                                m.regime.ForceUpdate(sym, m.mkt)
                        }
                }

                acc := m.currentAccount()
                openPnL := 0.0
                for _, b := range m.bots {
                        b.Tick(m.mkt, acc.Balance)
                        acc.Balance += b.TotalPnL - totalPnLBefore(b)
                        if b.OpenTrade != nil {
                                p := m.mkt.GetPrice(b.OpenTrade.Symbol)
                                if p != nil {
                                        if b.OpenTrade.Side == botpkg.Buy {
                                                openPnL += (p.Price - b.OpenTrade.EntryPrice) * b.OpenTrade.Quantity
                                        } else {
                                                openPnL += (b.OpenTrade.EntryPrice - p.Price) * b.OpenTrade.Quantity
                                        }
                                }
                        }
                }
                acc.Equity = acc.Balance + openPnL

                // ── Periodic: log drawdown snapshot setiap 5 menit ───────────────────
                if m.log != nil && time.Since(m.lastDrawdownLog) >= 5*time.Minute {
                        m.lastDrawdownLog = time.Now()
                        if acc.Equity > m.peakEquity {
                                m.peakEquity = acc.Equity
                        }
                        if m.peakEquity > 0 {
                                drawdownPct := (m.peakEquity - acc.Equity) / m.peakEquity * 100
                                m.log.DrawdownSnapshot(acc.Equity, m.peakEquity, drawdownPct)
                        }
                }

                // ── Periodic: log daily P&L saat pergantian hari ─────────────────────
                now := time.Now()
                if m.log != nil && (now.YearDay() != m.lastDailyLog.YearDay() || now.Year() != m.lastDailyLog.Year()) {
                        m.lastDailyLog = now
                        todayProfit, todayLoss, winRate := calcDailyStats(m.bots)
                        m.log.DailyPL(todayProfit, todayLoss, winRate)
                }

                return m, tickCmd()

        case tea.KeyMsg:
                if m.viewMode == ViewBotForm {
                        return m.updateBotForm(msg)
                }
                if m.viewMode == ViewConfirmSwitch {
                        return m.updateConfirmSwitch(msg)
                }

                switch msg.String() {
                case "ctrl+c", "q":
                        if m.log != nil {
                                totalPnL := 0.0
                                totalTrades := 0
                                for _, b := range m.bots {
                                        totalPnL += b.TotalPnL
                                        totalTrades += b.TradeCount()
                                }
                                m.log.SessionEnd(totalPnL, totalTrades)
                                m.log.Close()
                        }
                        return m, tea.Quit
                case "r":
                        if m.activeTab == TabSettings {
                                m.viewMode = ViewConfirmSwitch
                        }
                case "tab", "right":
                        m.activeTab = (m.activeTab + 1) % Tab(len(tabNames))
                case "shift+tab", "left":
                        m.activeTab = (m.activeTab - 1 + Tab(len(tabNames))) % Tab(len(tabNames))
                case "1":
                        m.activeTab = TabDashboard
                case "2":
                        m.activeTab = TabMarkets
                case "3":
                        m.activeTab = TabBots
                case "4":
                        m.activeTab = TabTrades
                case "5":
                        m.activeTab = TabSettings
                case "6":
                        m.activeTab = TabMetrics

                // Bot tab actions
                case "j", "down":
                        if m.activeTab == TabBots && m.selectedBot < len(m.bots)-1 {
                                m.selectedBot++
                        }
                case "k", "up":
                        if m.activeTab == TabBots && m.selectedBot > 0 {
                                m.selectedBot--
                        }
                case "s":
                        if m.activeTab == TabBots && len(m.bots) > 0 {
                                b := m.bots[m.selectedBot]
                                b.Toggle()
                                if m.log != nil {
                                        if b.IsRunning {
                                                m.log.BotStart(b.ID, b.Name, b.Symbol, string(b.Strategy))
                                        } else {
                                                m.log.BotStop(b.ID, b.Name, b.TotalPnL, b.WinCount, b.LossCount)
                                        }
                                }
                        }
                case "n":
                        if m.activeTab == TabBots {
                                m.initBotForm()
                                m.botFormEditing = false
                                m.botFormBotID = 0
                                m.viewMode = ViewBotForm
                        }
                case "e":
                        if m.activeTab == TabBots && len(m.bots) > 0 {
                                m.initBotFormFromBot(m.bots[m.selectedBot])
                                m.botFormEditing = true
                                m.botFormBotID = m.bots[m.selectedBot].ID
                                m.viewMode = ViewBotForm
                        }
                case "d":
                        if m.activeTab == TabBots && len(m.bots) > 0 {
                                m.bots = append(m.bots[:m.selectedBot], m.bots[m.selectedBot+1:]...)
                                if m.selectedBot > 0 {
                                        m.selectedBot--
                                }
                                _ = config.SaveBots(m.bots)
                        }
                }

        case telegramCmdMsg:
                if m.tgBot != nil {
                        m.handleTelegramCmd(telegram.Command(msg))
                }
                return m, waitTelegramCmd(m.tgBot.CmdChan())
        }
        return m, nil
}

var botPnLCache = make(map[int]float64)

func totalPnLBefore(b *botpkg.Bot) float64 {
        prev := botPnLCache[b.ID]
        botPnLCache[b.ID] = b.TotalPnL
        return prev
}

// handleTelegramCmd processes a command received from the Telegram bot and
// sends a reply back to the user via the Telegram API.
func (m *Model) handleTelegramCmd(cmd telegram.Command) {
        tg := m.tgBot
        acc := m.currentAccount()

        switch cmd.Type {
        case telegram.CmdHelp:
                tg.Send(telegram.HelpText())

        case telegram.CmdBalance:
                mode := "DEMO"
                if m.useReal {
                        mode = "REAL"
                }
                tg.Send(fmt.Sprintf(
                        "💰 <b>Saldo Akun (%s)</b>\n"+
                                "Balance: <b>$%.2f</b>\n"+
                                "Equity:  <b>$%.2f</b>",
                        mode, acc.Balance, acc.Equity,
                ))

        case telegram.CmdStatus:
                mt5Stat := m.mt5Client.Status.String()
                running := 0
                for _, b := range m.bots {
                        if b.IsRunning {
                                running++
                        }
                }
                mode := "DEMO"
                if m.dryRun {
                        mode = "DRY-RUN"
                }
                tg.Send(fmt.Sprintf(
                        "📊 <b>Status Finex CLI</b>\n"+
                                "Mode: %s\n"+
                                "MT5: %s\n"+
                                "Bot aktif: %d / %d\n"+
                                "Balance: $%.2f | Equity: $%.2f",
                        mode, mt5Stat,
                        running, len(m.bots),
                        acc.Balance, acc.Equity,
                ))

        case telegram.CmdBots:
                if len(m.bots) == 0 {
                        tg.Send("Tidak ada bot yang terdaftar.")
                        return
                }
                lines := "🤖 <b>Daftar Bot</b>\n\n"
                for _, b := range m.bots {
                        status := "⏹ STOP"
                        if b.IsRunning {
                                status = "▶️ JALAN"
                        }
                        openInfo := ""
                        if b.OpenTrade != nil {
                                openInfo = fmt.Sprintf(" | Open: %s %.5f", string(b.OpenTrade.Side), b.OpenTrade.EntryPrice)
                        }
                        lines += fmt.Sprintf(
                                "%s <b>%s</b> (%s)\n"+
                                        "  %s | P&amp;L: $%+.2f | W:%d L:%d%s\n\n",
                                status, b.Name, b.Symbol,
                                string(b.Strategy), b.TotalPnL, b.WinCount, b.LossCount,
                                openInfo,
                        )
                }
                tg.Send(lines)

        case telegram.CmdStart:
                arg := strings.TrimSpace(cmd.Arg)
                found := false
                for _, b := range m.bots {
                        if strings.EqualFold(b.Name, arg) {
                                found = true
                                if b.IsRunning {
                                        tg.Send(fmt.Sprintf("⚠️ Bot <b>%s</b> sudah berjalan.", b.Name))
                                } else {
                                        b.IsRunning = true
                                        if m.log != nil {
                                                m.log.BotStart(b.ID, b.Name, b.Symbol, string(b.Strategy))
                                        }
                                        tg.Send(fmt.Sprintf("▶️ Bot <b>%s</b> (%s) dimulai.", b.Name, b.Symbol))
                                }
                                break
                        }
                }
                if !found {
                        if arg == "" {
                                tg.Send("⚠️ Gunakan: /startbot &lt;nama bot&gt;")
                        } else {
                                tg.Send(fmt.Sprintf("❓ Bot <b>%s</b> tidak ditemukan. Ketik /bots untuk daftar bot.", arg))
                        }
                }

        case telegram.CmdStop:
                arg := strings.TrimSpace(cmd.Arg)
                found := false
                for _, b := range m.bots {
                        if strings.EqualFold(b.Name, arg) {
                                found = true
                                if !b.IsRunning {
                                        tg.Send(fmt.Sprintf("⚠️ Bot <b>%s</b> sudah berhenti.", b.Name))
                                } else {
                                        b.IsRunning = false
                                        if m.log != nil {
                                                m.log.BotStop(b.ID, b.Name, b.TotalPnL, b.WinCount, b.LossCount)
                                        }
                                        tg.Send(fmt.Sprintf("⏹ Bot <b>%s</b> (%s) dihentikan.", b.Name, b.Symbol))
                                }
                                break
                        }
                }
                if !found {
                        if arg == "" {
                                tg.Send("⚠️ Gunakan: /stopbot &lt;nama bot&gt;")
                        } else {
                                tg.Send(fmt.Sprintf("❓ Bot <b>%s</b> tidak ditemukan. Ketik /bots untuk daftar bot.", arg))
                        }
                }

        case telegram.CmdTrades:
                openCount := 0
                lines := "📈 <b>Posisi Terbuka</b>\n\n"
                for _, b := range m.bots {
                        if b.OpenTrade == nil {
                                continue
                        }
                        openCount++
                        t := b.OpenTrade
                        p := m.mkt.GetPrice(t.Symbol)
                        unrealized := 0.0
                        if p != nil {
                                if t.Side == botpkg.Buy {
                                        unrealized = (p.Price - t.EntryPrice) * t.Quantity
                                } else {
                                        unrealized = (t.EntryPrice - p.Price) * t.Quantity
                                }
                        }
                        dur := telegram.FormatDuration(time.Since(t.OpenedAt))
                        lines += fmt.Sprintf(
                                "<b>%s</b> %s %s\n"+
                                        "Entry: %.5f | Unrealized: <b>$%+.2f</b>\n"+
                                        "Durasi: %s\n\n",
                                b.Name, t.Symbol, string(t.Side),
                                t.EntryPrice, unrealized,
                                dur,
                        )
                }
                if openCount == 0 {
                        tg.Send("📈 Tidak ada posisi terbuka saat ini.")
                } else {
                        tg.Send(lines)
                }

        case telegram.CmdUnknown:
                tg.Send("❓ Perintah tidak dikenal. Ketik /help untuk daftar perintah.")
        }
}

func (m Model) updateConfirmSwitch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
        switch msg.String() {
        case "y", "enter":
                m.useReal = !m.useReal
                m.viewMode = ViewList
        case "n", "esc":
                m.viewMode = ViewList
        }
        return m, nil
}

// ─── Bot Form ─────────────────────────────────────────────────────────────────

var symbols = []string{
        "EURUSD", "GBPUSD", "USDJPY", "AUDUSD",
        "USDCAD", "USDCHF", "EURGBP", "EURJPY",
}

func (m *Model) initBotForm() {
        inputs := make([]textinput.Model, 4)
        labels := []string{"Bot Name", "Risk %", "Take Profit %", "Stop Loss %"}
        defaults := []string{"My Bot", "1.0", "2.0", "1.0"}
        for i := range inputs {
                t := textinput.New()
                t.Placeholder = labels[i]
                t.CharLimit = 30
                if i == 0 {
                        t.Focus()
                }
                t.SetValue(defaults[i])
                inputs[i] = t
        }
        m.botFormInputs = inputs
        m.botFormFocused = 0
        m.botFormSymIdx = 0
        m.botFormStrIdx = 0
}

func (m *Model) initBotFormFromBot(b *botpkg.Bot) {
        m.initBotForm()
        m.botFormInputs[0].SetValue(b.Name)
        m.botFormInputs[1].SetValue(fmt.Sprintf("%.1f", b.RiskPct))
        m.botFormInputs[2].SetValue(fmt.Sprintf("%.1f", b.TakeProfitPct))
        m.botFormInputs[3].SetValue(fmt.Sprintf("%.1f", b.StopLossPct))
        for i, s := range symbols {
                if s == b.Symbol {
                        m.botFormSymIdx = i
                        break
                }
        }
        for i, s := range botpkg.AllStrategies {
                if s == b.Strategy {
                        m.botFormStrIdx = i
                        break
                }
        }
}

func (m Model) updateBotForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
        totalFields := len(m.botFormInputs) + 2 // +symbol +strategy

        switch msg.String() {
        case "esc":
                m.viewMode = ViewList
                return m, nil
        case "enter", "ctrl+s":
                m.saveBotForm()
                m.viewMode = ViewList
                return m, nil
        case "tab", "down":
                m.botFormInputs[m.botFormFocused%len(m.botFormInputs)].Blur()
                m.botFormFocused = (m.botFormFocused + 1) % totalFields
                if m.botFormFocused < len(m.botFormInputs) {
                        m.botFormInputs[m.botFormFocused].Focus()
                }
        case "shift+tab", "up":
                if m.botFormFocused < len(m.botFormInputs) {
                        m.botFormInputs[m.botFormFocused].Blur()
                }
                m.botFormFocused = (m.botFormFocused - 1 + totalFields) % totalFields
                if m.botFormFocused < len(m.botFormInputs) {
                        m.botFormInputs[m.botFormFocused].Focus()
                }
        case "left":
                if m.botFormFocused == len(m.botFormInputs) {
                        m.botFormSymIdx = (m.botFormSymIdx - 1 + len(symbols)) % len(symbols)
                } else if m.botFormFocused == len(m.botFormInputs)+1 {
                        m.botFormStrIdx = (m.botFormStrIdx - 1 + len(botpkg.AllStrategies)) % len(botpkg.AllStrategies)
                }
        case "right":
                if m.botFormFocused == len(m.botFormInputs) {
                        m.botFormSymIdx = (m.botFormSymIdx + 1) % len(symbols)
                } else if m.botFormFocused == len(m.botFormInputs)+1 {
                        m.botFormStrIdx = (m.botFormStrIdx + 1) % len(botpkg.AllStrategies)
                }
        }

        var cmds []tea.Cmd
        for i := range m.botFormInputs {
                var cmd tea.Cmd
                m.botFormInputs[i], cmd = m.botFormInputs[i].Update(msg)
                cmds = append(cmds, cmd)
        }
        return m, tea.Batch(cmds...)
}

func (m *Model) saveBotForm() {
        name := m.botFormInputs[0].Value()
        risk := parseFloat(m.botFormInputs[1].Value(), 1.0)
        tp := parseFloat(m.botFormInputs[2].Value(), 2.0)
        sl := parseFloat(m.botFormInputs[3].Value(), 1.0)
        sym := symbols[m.botFormSymIdx]
        strat := botpkg.AllStrategies[m.botFormStrIdx]

        if m.botFormEditing {
                for _, b := range m.bots {
                        if b.ID == m.botFormBotID {
                                b.Name = name
                                b.Symbol = sym
                                b.Strategy = strat
                                b.RiskPct = risk
                                b.TakeProfitPct = tp
                                b.StopLossPct = sl
                                break
                        }
                }
        } else {
                newID := len(m.bots) + 1
                b := botpkg.NewBot(newID, name, sym, strat, risk, tp, sl)
                m.bots = append(m.bots, b)
                m.selectedBot = len(m.bots) - 1
                // Wire trade log callbacks for the new bot
                b.OnTradeOpen = func(ev botpkg.TradeEvent) {
                        if m.log != nil {
                                m.log.TradeOpen(ev.Bot.ID, ev.Bot.Name, ev.Trade.Symbol, string(ev.Trade.Side), ev.Trade.ID, ev.Trade.EntryPrice, ev.Trade.Quantity)
                        }
                }
                b.OnTradeClose = func(ev botpkg.TradeEvent) {
                        if m.log != nil {
                                m.log.TradeClose(ev.Bot.ID, ev.Bot.Name, ev.Trade.Symbol, string(ev.Trade.Side), ev.Trade.ID, ev.Trade.EntryPrice, ev.Trade.ExitPrice, ev.Trade.PnL, ev.Trade.Quantity, ev.Trade.OpenedAt, ev.Trade.ClosedAt)
                        }
                }
        }
        // Persist bot list to bots.json after every create/edit
        _ = config.SaveBots(m.bots)
}

func parseFloat(s string, def float64) float64 {
        var v float64
        _, err := fmt.Sscanf(s, "%f", &v)
        if err != nil || v <= 0 {
                return def
        }
        return v
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
        if m.width == 0 {
                return "Loading Finex Trading Bot..."
        }

        header := m.renderHeader()
        tabs := m.renderTabs()
        var content string

        switch m.viewMode {
        case ViewBotForm:
                content = m.renderBotForm()
        case ViewConfirmSwitch:
                content = m.renderConfirmSwitch()
        default:
                switch m.activeTab {
                case TabDashboard:
                        content = m.renderDashboard()
                case TabMarkets:
                        content = m.renderMarkets()
                case TabBots:
                        content = m.renderBots()
                case TabTrades:
                        content = m.renderTrades()
                case TabSettings:
                        content = m.renderSettings()
                case TabMetrics:
                        content = m.renderMetrics()
                }
        }

        help := m.renderHelp()
        helpSep := lipgloss.NewStyle().Foreground(colorBorder).Background(colorBg).Render(strings.Repeat("─", m.width))

        full := lipgloss.JoinVertical(lipgloss.Left,
                header,
                tabs,
                content,
                helpSep,
                help,
        )

        return baseStyle.Width(m.width).Render(full)
}

func (m Model) renderHeader() string {
        acc := m.currentAccount()

        modeTag := demoTagStyle.Render(" DEMO ")
        if m.useReal {
                modeTag = realTagStyle.Render(" LIVE ")
        }

        var connBadge string
        switch m.mt5Status {
        case mt5.StatusConnected:
                connBadge = lipgloss.NewStyle().
                        Background(colorGreen).Foreground(colorBg).Bold(true).Padding(0, 1).
                        Render(" MT5 ✓ ")
        case mt5.StatusConnecting, mt5.StatusHandshake, mt5.StatusAuthenticating:
                connBadge = lipgloss.NewStyle().
                        Background(colorYellow).Foreground(colorBg).Bold(true).Padding(0, 1).
                        Render(" MT5 … ")
        case mt5.StatusFailed:
                if !m.mt5NextRetry.IsZero() {
                        secs := int(time.Until(m.mt5NextRetry).Seconds())
                        if secs < 0 {
                                secs = 0
                        }
                        connBadge = lipgloss.NewStyle().
                                Background(colorRed).Foreground(colorBg).Bold(true).Padding(0, 1).
                                Render(fmt.Sprintf(" MT5 ↻%ds ", secs))
                } else {
                        connBadge = lipgloss.NewStyle().
                                Background(colorRed).Foreground(colorBg).Bold(true).Padding(0, 1).
                                Render(" MT5 ✗ ")
                }
        default:
                connBadge = lipgloss.NewStyle().
                        Background(colorMuted).Foreground(colorBg).Padding(0, 1).
                        Render(" MT5 -- ")
        }

        logo := lipgloss.NewStyle().
                Foreground(colorPrimary).Bold(true).
                Render("◈ FINEX")
        ver := mutedStyle.Render(" v1.0")
        clockStr := lipgloss.NewStyle().
                Foreground(colorWhite).
                Render(time.Now().Format("15:04:05"))
        clockLabel := mutedStyle.Render("  ▶ ")

        balStr := lipgloss.NewStyle().Foreground(colorWhite).
                Render(fmt.Sprintf("$%.2f %s", acc.Balance, acc.Currency))
        eqStr := mutedStyle.Render(fmt.Sprintf("Eq: $%.2f", acc.Equity))

        // Compute floating (unrealized) P&L for header display
        floatPnLHeader := 0.0
        for _, b := range m.bots {
                if b.OpenTrade != nil {
                        hp := m.mkt.GetPrice(b.OpenTrade.Symbol)
                        if hp != nil {
                                if b.OpenTrade.Side == botpkg.Buy {
                                        floatPnLHeader += (hp.Price - b.OpenTrade.EntryPrice) * b.OpenTrade.Quantity
                                } else {
                                        floatPnLHeader += (b.OpenTrade.EntryPrice - hp.Price) * b.OpenTrade.Quantity
                                }
                        }
                }
        }
        var floatStr string
        if floatPnLHeader != 0 {
                floatStr = "  " + pnlStyle(floatPnLHeader).Render(fmt.Sprintf("Float %+.2f", floatPnLHeader))
        }

        right := lipgloss.JoinHorizontal(lipgloss.Center,
                eqStr, "  ", balStr, floatStr, "  ", modeTag, "  ", connBadge,
        )
        center := clockLabel + clockStr

        leftW := lipgloss.Width(logo + ver)
        rightW := lipgloss.Width(right)
        centerW := lipgloss.Width(center)
        gapL := (m.width/2 - leftW - centerW/2)
        if gapL < 1 {
                gapL = 1
        }
        gapR := m.width - leftW - gapL - centerW - rightW - 4
        if gapR < 1 {
                gapR = 1
        }

        topBar := lipgloss.NewStyle().
                Background(colorSurface).
                Foreground(colorWhite).
                Width(m.width).
                Padding(0, 2).
                Render(logo + ver + strings.Repeat(" ", gapL) + center + strings.Repeat(" ", gapR) + right)

        sepLine := lipgloss.NewStyle().
                Foreground(colorBorder).
                Background(colorBg).
                Render(strings.Repeat("─", m.width))

        return topBar + "\n" + sepLine
}

func (m Model) renderTabs() string {
        var parts []string
        for i, name := range tabNames {
                num := fmt.Sprintf("%d", i+1)
                if Tab(i) == m.activeTab {
                        label := fmt.Sprintf("  [%s] %s  ", num, strings.ToUpper(name))
                        parts = append(parts, tabActiveStyle.Render(label))
                } else {
                        label := fmt.Sprintf("  [%s] %s  ", num, name)
                        parts = append(parts, tabInactiveStyle.Render(label))
                }
        }
        bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
        barW := lipgloss.Width(bar)
        if barW < m.width {
                bar += lipgloss.NewStyle().Background(colorSurface).Width(m.width - barW).Render("")
        }
        sep := lipgloss.NewStyle().Foreground(colorBorder).Background(colorBg).Render(strings.Repeat("─", m.width))
        return bar + "\n" + sep
}

func (m Model) renderDashboard() string {
        acc := m.currentAccount()

        totalPnL := 0.0
        totalTrades := 0
        wins := 0
        activeBots := 0
        openTrades := 0
        floatPnL := 0.0

        for _, b := range m.bots {
                totalPnL += b.TotalPnL
                totalTrades += b.TradeCount()
                wins += b.WinCount
                if b.IsRunning {
                        activeBots++
                }
                if b.OpenTrade != nil {
                        openTrades++
                        p := m.mkt.GetPrice(b.OpenTrade.Symbol)
                        if p != nil {
                                if b.OpenTrade.Side == botpkg.Buy {
                                        floatPnL += (p.Price - b.OpenTrade.EntryPrice) * b.OpenTrade.Quantity
                                } else {
                                        floatPnL += (b.OpenTrade.EntryPrice - p.Price) * b.OpenTrade.Quantity
                                }
                        }
                }
        }
        winRate := 0.0
        if totalTrades > 0 {
                winRate = float64(wins) / float64(totalTrades) * 100
        }

        // ── KPI tiles ──────────────────────────────────────────────────────────
        innerW := m.width - 4
        tileW := (innerW - 3) / 4
        if tileW < 10 {
                tileW = 10
        }

        balTile := kpiTile("  Balance",
                fmt.Sprintf("  $%.2f", acc.Balance),
                tileW,
                lipgloss.NewStyle().Foreground(colorWhite).Bold(true))

        eqTile := kpiTile("  Equity",
                fmt.Sprintf("  $%.2f", acc.Equity),
                tileW,
                lipgloss.NewStyle().Foreground(colorWhite))

        pnlLabel := "  Session P&L"
        if openTrades > 0 {
                pnlLabel = fmt.Sprintf("  P&L  (Float %+.2f)", floatPnL)
        }
        pnlTile := kpiTile(pnlLabel,
                fmt.Sprintf("  %+.2f USD", totalPnL),
                tileW,
                pnlStyle(totalPnL))

        wrTile := kpiTile(
                fmt.Sprintf("  Win Rate  Bots %d/%d", activeBots, len(m.bots)),
                fmt.Sprintf("  %s %.0f%%  Trades: %d", winBar(winRate, 8), winRate, totalTrades),
                tileW,
                lipgloss.NewStyle().Foreground(colorGreen))

        kpiRow := lipgloss.JoinHorizontal(lipgloss.Top, balTile, " ", eqTile, " ", pnlTile, " ", wrTile)

        // ── Bot status table (1 line per bot) ────────────────────────────────
        hdrLine := fmt.Sprintf("  %-18s %-8s %-20s %-12s %-10s  %s",
                titleStyle.Render("Bot"),
                titleStyle.Render("Symbol"),
                titleStyle.Render("Strategy"),
                titleStyle.Render("Status"),
                titleStyle.Render("P&L"),
                titleStyle.Render("Win %"),
        )
        botLines := []string{hdrLine, mutedStyle.Render("  " + strings.Repeat("─", m.width-10))}
        for _, b := range m.bots {
                statusStr := redStyle.Render("● STOPPED")
                if b.IsRunning {
                        statusStr = greenStyle.Render("● RUNNING")
                }
                wrPct := fmt.Sprintf("%.0f%%", b.WinRate())
                line := fmt.Sprintf("  %-18s %-8s %-20s %-12s %-10s  %s",
                        b.Name, b.Symbol, string(b.Strategy), statusStr,
                        pnlStyle(b.TotalPnL).Render(fmt.Sprintf("%+.2f", b.TotalPnL)),
                        mutedStyle.Render(wrPct),
                )
                botLines = append(botLines, line)
        }
        botSection := cardStyle.Width(m.width - 4).Render(strings.Join(botLines, "\n"))

        // ── Market snapshot: all 8 pairs in 2 columns (full-width card) ────────
        prices := m.mkt.GetAllPrices()
        colW := (innerW - 6) / 2
        mktLines := []string{
                titleStyle.Render("  Market Prices") + mutedStyle.Render("                          (all pairs, live)"),
                mutedStyle.Render("  " + strings.Repeat("─", innerW-8)),
        }
        for i := 0; i < 4 && i < len(prices); i++ {
                left := prices[i]
                lDir, lSt := greenStyle, greenStyle
                lArrow := "▲"
                if left.Change < 0 {
                        lDir, lSt = redStyle, redStyle
                        lArrow = "▼"
                }
                lStr := fmt.Sprintf("  %-8s %s %10.5f  %s",
                        left.Symbol,
                        lDir.Render(lArrow),
                        left.Price,
                        lSt.Render(fmt.Sprintf("%+.2f%%", left.ChangePct)))
                lPad := colW - lipgloss.Width(lStr)
                if lPad < 2 {
                        lPad = 2
                }

                rStr := ""
                if i+4 < len(prices) {
                        right := prices[i+4]
                        rDir, rSt := greenStyle, greenStyle
                        rArrow := "▲"
                        if right.Change < 0 {
                                rDir, rSt = redStyle, redStyle
                                rArrow = "▼"
                        }
                        rStr = fmt.Sprintf("  %-8s %s %10.5f  %s",
                                right.Symbol,
                                rDir.Render(rArrow),
                                right.Price,
                                rSt.Render(fmt.Sprintf("%+.2f%%", right.ChangePct)))
                }
                mktLines = append(mktLines, lStr+strings.Repeat(" ", lPad)+rStr)
        }
        mktCard := cardStyle.Width(innerW).Render(strings.Join(mktLines, "\n"))

        // ── Recent trades (full-width, compact) ──────────────────────────────
        allTrades := []*botpkg.Trade{}
        for _, b := range m.bots {
                allTrades = append(allTrades, b.Trades...)
        }
        recentHdr := titleStyle.Render("  Recent Trades") +
                mutedStyle.Render(fmt.Sprintf("  (%d total)", len(allTrades)))
        recentLines := []string{recentHdr}
        shown := 0
        for i := len(allTrades) - 1; i >= 0 && shown < 4; i-- {
                tr := allTrades[i]
                side := greenStyle.Render("BUY ")
                if tr.Side == botpkg.Sell {
                        side = redStyle.Render("SELL")
                }
                statusSt := yellowStyle
                if tr.Status == botpkg.Closed {
                        statusSt = mutedStyle
                }
                botName := ""
                for _, b := range m.bots {
                        if b.ID == tr.BotID {
                                botName = b.Name
                        }
                }
                line := fmt.Sprintf("  %-14s  %-8s  %s  %-8s  %s",
                        mutedStyle.Render(botName),
                        tr.Symbol, side,
                        statusSt.Render(string(tr.Status)),
                        pnlStyle(tr.PnL).Render(fmt.Sprintf("%+.2f USD", tr.PnL)))
                recentLines = append(recentLines, line)
                shown++
        }
        if shown == 0 {
                recentLines = append(recentLines, "")
                recentLines = append(recentLines, mutedStyle.Render("  No trades yet — start a bot to begin."))
                recentLines = append(recentLines, yellowStyle.Render("  → Tab [3] Bots  →  [s] Start"))
        }
        recentCard := cardStyle.Width(innerW).Render(strings.Join(recentLines, "\n"))

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        kpiRow, " ", botSection, " ", mktCard, " ", recentCard,
                ),
        )
}

func pnlColor(v float64) lipgloss.Color {
        if v > 0 {
                return colorGreen
        } else if v < 0 {
                return colorRed
        }
        return colorMuted
}

// ─── Markets ──────────────────────────────────────────────────────────────────

// pctB computes the Bollinger %B value: 0 = at lower band, 100 = at upper band.
// Returns 50 when bands are flat or not enough data.
func pctB(closes []float64) float64 {
        if len(closes) == 0 {
                return 50
        }
        _, upper, lower := indicator.BollingerBands(closes, 20, 2.0)
        if upper == lower {
                return 50
        }
        price := closes[len(closes)-1]
        v := (price - lower) / (upper - lower) * 100
        if v < 0 {
                return 0
        }
        if v > 100 {
                return 100
        }
        return v
}

// renderRSIBar draws a 10-block filled bar representing RSI 0–100.
// Color zones: green < 38 (oversold), yellow 38–62 (neutral), red > 62 (overbought).
func renderRSIBar(rsi float64) string {
        const width = 10
        filled := int(rsi/10 + 0.5)
        if filled < 0 {
                filled = 0
        }
        if filled > width {
                filled = width
        }
        var sb strings.Builder
        for i := 0; i < width; i++ {
                if i < filled {
                        sb.WriteString("▓")
                } else {
                        sb.WriteString("░")
                }
        }
        bar := sb.String()
        var col lipgloss.Color
        switch {
        case rsi < 38:
                col = colorGreen
        case rsi > 62:
                col = colorRed
        default:
                col = colorYellow
        }
        return lipgloss.NewStyle().Foreground(col).Render(bar)
}

// renderBBBar draws a 12-slot bar showing price position within Bollinger Bands.
// ● marks the current price; < 20% = green (near lower), > 80% = red (near upper).
func renderBBBar(pb float64) string {
        const slots = 12
        pos := int(pb/100*float64(slots-1) + 0.5)
        if pos < 0 {
                pos = 0
        }
        if pos >= slots {
                pos = slots - 1
        }
        var sb strings.Builder
        for i := 0; i < slots; i++ {
                if i == pos {
                        sb.WriteString("●")
                } else {
                        sb.WriteString("─")
                }
        }
        bar := sb.String()
        var col lipgloss.Color
        switch {
        case pb < 20:
                col = colorGreen
        case pb > 80:
                col = colorRed
        default:
                col = colorMuted
        }
        return lipgloss.NewStyle().Foreground(col).Render(bar)
}

// signalInfo returns a styled label + arrow for a given indicator.Signal.
func signalInfo(sig indicator.Signal) string {
        switch sig {
        case indicator.Long:
                return greenStyle.Render("LONG  ↑")
        case indicator.Short:
                return redStyle.Render("SHORT ↓")
        default:
                return mutedStyle.Render("WAIT  –")
        }
}

func (m Model) renderMarkets() string {
        prices := m.mkt.GetAllPrices()
        cardW := m.width - 6

        // ── Indicator table ──────────────────────────────────────────────────────
        hdr := fmt.Sprintf("  %-8s %-10s %-8s   %-22s %-18s  %-9s",
                "Symbol", "Price", "Chg%", "RSI(7)  [bar]  val", "[%B 2σ]  val", "Signal")
        var rows []string
        rows = append(rows,
                titleStyle.Render(hdr),
                mutedStyle.Render("  "+strings.Repeat("─", cardW-6)),
        )

        for _, p := range prices {
                closes := m.mkt.GetCloses(p.Symbol)

                // RSI(7)
                rsiVal := indicator.RSI(closes, 7)
                rsiBar := renderRSIBar(rsiVal)

                // Bollinger %B
                pb := pctB(closes)
                bbBar := renderBBBar(pb)

                // Composite signal: all 4 strategies — show the dominant one.
                // Priority: Scalping > MeanReversion > TrendFollowing > SwingTrading.
                sig := indicator.ScalpingSignal(closes)
                if sig == indicator.None {
                        sig = indicator.MeanReversionSignal(closes)
                }
                if sig == indicator.None {
                        sig = indicator.TrendSignal(closes)
                }
                if sig == indicator.None {
                        sig = indicator.SwingSignal(closes)
                }

                // Change %
                chgStyle := greenStyle
                chgArrow := "▲"
                if p.Change < 0 {
                        chgStyle = redStyle
                        chgArrow = "▼"
                }
                chgStr := chgStyle.Render(fmt.Sprintf("%s%+.2f%%", chgArrow, p.ChangePct))

                row := fmt.Sprintf("  %-8s %-10.4f %-16s  %s %-5.1f   [%s] %4.0f%%   %s",
                        p.Symbol,
                        p.Price,
                        chgStr,
                        rsiBar,
                        rsiVal,
                        bbBar,
                        pb,
                        signalInfo(sig),
                )
                rows = append(rows, row)
        }

        indicatorCard := cardStyle.Width(cardW).Render(
                titleStyle.Render("  Live Indicators\n") +
                        strings.Join(rows, "\n"),
        )

        // ── Legend ───────────────────────────────────────────────────────────────
        legendLine1 := fmt.Sprintf("  RSI(7): %s < 38 Oversold   %s 38–62 Neutral   %s > 62 Overbought",
                greenStyle.Render("▓▓▓▓▓▓▓▓▓▓"),
                yellowStyle.Render("▓▓▓▓▓▓▓▓▓▓"),
                redStyle.Render("▓▓▓▓▓▓▓▓▓▓"),
        )
        legendLine2 := fmt.Sprintf("  %%B:    %s < 20%% Near lower band   %s 20–80%% Inside bands   %s > 80%% Near upper band",
                greenStyle.Render("●"),
                mutedStyle.Render("●"),
                redStyle.Render("●"),
        )
        legendLine3 := fmt.Sprintf("  Signal: %s Buy  %s Sell  %s No setup — composite of Scalp/MeanRev/Trend/Swing",
                greenStyle.Render("LONG ↑"),
                redStyle.Render("SHORT ↓"),
                mutedStyle.Render("WAIT –"),
        )
        legendCard := cardStyle.Width(cardW).Render(
                titleStyle.Render("  Legend\n") +
                        legendLine1 + "\n" + legendLine2 + "\n" + legendLine3,
        )

        // ── EURUSD sparkline ─────────────────────────────────────────────────────
        history := m.mkt.GetHistory("EURUSD")
        sparkTitle := titleStyle.Render("  EURUSD — Price Chart (last 30 candles)")
        spark := renderSparkline(history, cardW-6)
        sparkCard := cardStyle.Width(cardW).Render(sparkTitle + "\n\n" + spark)

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        indicatorCard, " ",
                        legendCard, " ",
                        sparkCard,
                ),
        )
}

func renderSparkline(candles []market.Candle, width int) string {
        if len(candles) == 0 {
                return ""
        }
        n := width / 2
        if n > len(candles) {
                n = len(candles)
        }
        data := candles[len(candles)-n:]

        minP, maxP := data[0].Close, data[0].Close
        for _, c := range data {
                if c.Close < minP {
                        minP = c.Close
                }
                if c.Close > maxP {
                        maxP = c.Close
                }
        }
        rang := maxP - minP
        if rang == 0 {
                rang = 1
        }

        bars := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
        var out strings.Builder
        out.WriteString("  ")
        for i, c := range data {
                idx := int((c.Close - minP) / rang * float64(len(bars)-1))
                if idx < 0 {
                        idx = 0
                }
                if idx >= len(bars) {
                        idx = len(bars) - 1
                }
                col := colorGreen
                if i > 0 && c.Close < data[i-1].Close {
                        col = colorRed
                }
                out.WriteString(lipgloss.NewStyle().Foreground(col).Render(bars[idx]))
        }
        out.WriteString(fmt.Sprintf("  %.5f – %.5f", minP, maxP))
        return out.String()
}

// ─── Bots ─────────────────────────────────────────────────────────────────────

func (m Model) renderBots() string {
        if len(m.bots) == 0 {
                empty := cardStyle.Width(m.width - 4).Render(
                        lipgloss.JoinVertical(lipgloss.Center,
                                mutedStyle.Render("No bots configured yet."),
                                "",
                                yellowStyle.Render("Press [n] to create your first bot"),
                        ),
                )
                return lipgloss.NewStyle().Padding(1, 2).Render(empty)
        }

        innerW := m.width - 4

        // ── Compact bot list table ───────────────────────────────────────────
        hdr := fmt.Sprintf("  %-3s %-18s %-8s %-20s %-12s %-10s %-6s",
                "", "Bot", "Symbol", "Strategy", "Status", "P&L", "Win%")
        rows := []string{
                titleStyle.Render(hdr),
                mutedStyle.Render("  " + strings.Repeat("─", innerW-8)),
        }
        for i, b := range m.bots {
                sel := "  "
                st := mutedStyle
                if i == m.selectedBot {
                        sel = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("▶ ")
                        st = lipgloss.NewStyle().Foreground(colorWhite)
                }
                statusStr := redStyle.Render("● STOP")
                if b.IsRunning {
                        statusStr = greenStyle.Render("● RUN ")
                }
                row := fmt.Sprintf("%s%-18s %-8s %-20s %-12s %-10s %s",
                        sel,
                        st.Render(b.Name),
                        lipgloss.NewStyle().Foreground(colorViolet).Render(b.Symbol),
                        mutedStyle.Render(string(b.Strategy)),
                        statusStr,
                        pnlStyle(b.TotalPnL).Render(fmt.Sprintf("%+.2f", b.TotalPnL)),
                        mutedStyle.Render(fmt.Sprintf("%.0f%%", b.WinRate())),
                )
                rows = append(rows, row)
        }
        listCard := cardStyle.Width(innerW).Render(strings.Join(rows, "\n"))

        // ── Expanded detail for selected bot ────────────────────────────────
        var detailCard string
        if m.selectedBot < len(m.bots) {
                b := m.bots[m.selectedBot]

                statusStr := redStyle.Render("● STOPPED")
                if b.IsRunning {
                        statusStr = greenStyle.Render("● RUNNING")
                }

                openStr := mutedStyle.Render("  No open trade")
                if b.OpenTrade != nil {
                        p := m.mkt.GetPrice(b.OpenTrade.Symbol)
                        if p != nil {
                                unrealized := 0.0
                                if b.OpenTrade.Side == botpkg.Buy {
                                        unrealized = (p.Price - b.OpenTrade.EntryPrice) * b.OpenTrade.Quantity
                                } else {
                                        unrealized = (b.OpenTrade.EntryPrice - p.Price) * b.OpenTrade.Quantity
                                }
                                sideStr := greenStyle.Render("BUY")
                                if b.OpenTrade.Side == botpkg.Sell {
                                        sideStr = redStyle.Render("SELL")
                                }
                                openStr = fmt.Sprintf("  Open %s @ %.5f   Qty: %.4f   Unrealized: %s",
                                        sideStr,
                                        b.OpenTrade.EntryPrice,
                                        b.OpenTrade.Quantity,
                                        pnlStyle(unrealized).Render(fmt.Sprintf("%+.2f USD", unrealized)),
                                )
                        }
                }

                sep := mutedStyle.Render("  │  ")
                row1 := lipgloss.JoinHorizontal(lipgloss.Center,
                        titleStyle.Render("  "+b.Name), "   ", statusStr,
                        "   ", mutedStyle.Render(b.Symbol+"  "+string(b.Strategy)),
                )
                row2 := fmt.Sprintf("  Risk %s  %s TP %s  %s SL %s  %s Trades: %s  %s Win Rate: %s",
                        yellowStyle.Render(fmt.Sprintf("%.1f%%", b.RiskPct)),
                        sep,
                        greenStyle.Render(fmt.Sprintf("%.1f%%", b.TakeProfitPct)),
                        sep,
                        redStyle.Render(fmt.Sprintf("%.1f%%", b.StopLossPct)),
                        sep,
                        mutedStyle.Render(fmt.Sprintf("%d", b.TradeCount())),
                        sep,
                        winBar(b.WinRate(), 10)+" "+mutedStyle.Render(fmt.Sprintf("%.0f%%", b.WinRate())),
                )
                row3 := fmt.Sprintf("  Total P&L: %s",
                        pnlStyle(b.TotalPnL).Render(fmt.Sprintf("%+.2f USD", b.TotalPnL)))

                detailCard = cardHiStyle.Width(innerW).Render(
                        strings.Join([]string{row1, row2, row3, openStr}, "\n"),
                )
        }

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left, listCard, " ", detailCard),
        )
}

func (m Model) renderTrades() string {
        allTrades := []*botpkg.Trade{}
        botNames := map[int]string{}
        for _, b := range m.bots {
                botNames[b.ID] = b.Name
                allTrades = append(allTrades, b.Trades...)
        }

        header := fmt.Sprintf("  %-14s %-12s %-6s %-12s %-12s %-12s %-8s",
                "Bot", "Symbol", "Side", "Entry", "Exit", "P&L (USD)", "Status")

        lines := []string{
                titleStyle.Render(header),
                mutedStyle.Render("  " + strings.Repeat("─", 86)),
        }

        if len(allTrades) == 0 {
                lines = append(lines, mutedStyle.Render("\n  No trades yet. Start a bot to begin trading!"))
        }

        // show newest first
        for i := len(allTrades) - 1; i >= 0 && i >= len(allTrades)-30; i-- {
                tr := allTrades[i]
                exit := "—"
                if tr.ExitPrice > 0 {
                        exit = fmt.Sprintf("%.4f", tr.ExitPrice)
                }
                pnlStr := mutedStyle.Render("—")
                if tr.Status == botpkg.Closed {
                        pnlStr = pnlStyle(tr.PnL).Render(fmt.Sprintf("%+.2f", tr.PnL))
                }
                sideStr := greenStyle.Render("BUY ")
                if tr.Side == botpkg.Sell {
                        sideStr = redStyle.Render("SELL")
                }
                statusStr := yellowStyle.Render("OPEN  ")
                if tr.Status == botpkg.Closed {
                        statusStr = mutedStyle.Render("CLOSED")
                }

                line := fmt.Sprintf("  %-14s %-12s %s  %-12.4f %-12s %-12s %s",
                        botNames[tr.BotID],
                        tr.Symbol,
                        sideStr,
                        tr.EntryPrice,
                        exit,
                        pnlStr,
                        statusStr,
                )
                lines = append(lines, line)
        }

        tableCard := cardStyle.Width(m.width - 4).Render(
                strings.Join(lines, "\n"),
        )

        // Summary
        totalPnL := 0.0
        wins, losses := 0, 0
        for _, tr := range allTrades {
                if tr.Status == botpkg.Closed {
                        totalPnL += tr.PnL
                        if tr.PnL >= 0 {
                                wins++
                        } else {
                                losses++
                        }
                }
        }
        wr := 0.0
        if wins+losses > 0 {
                wr = float64(wins) / float64(wins+losses) * 100
        }
        summary := cardStyle.Width(m.width - 4).Render(
                fmt.Sprintf(
                        "  Total Closed: %d  │  Wins: %s  │  Losses: %s  │  Win Rate: %s  │  Total P&L: %s",
                        wins+losses,
                        greenStyle.Render(fmt.Sprintf("%d", wins)),
                        redStyle.Render(fmt.Sprintf("%d", losses)),
                        greenStyle.Render(fmt.Sprintf("%.1f%%", wr)),
                        pnlStyle(totalPnL).Render(fmt.Sprintf("%+.2f USD", totalPnL)),
                ),
        )

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left, summary, " ", tableCard),
        )
}

// ─── Metrics ──────────────────────────────────────────────────────────────────

// metricsData holds computed performance statistics for the metrics tab.
type metricsData struct {
        winRate      float64
        profitFactor float64
        sharpe       float64
        maxDrawdown  float64
        curDrawdown  float64
        totalTrades  int

        // Per-pair profit totals (sorted descending by PnL).
        pairPnL []struct {
                symbol string
                pnl    float64
                trades int
        }

        // Per-strategy profit factor (sorted descending).
        stratPF []struct {
                strategy string
                pf       float64
                winRate  float64
                trades   int
        }
}

// computeMetrics aggregates trade data from all bots into a metricsData struct.
func computeMetrics(bots []*botpkg.Bot, peakEquity, currentEquity float64) metricsData {
        var allPnL []float64
        totalWinPnL, totalLossPnL := 0.0, 0.0
        wins, totalTrades := 0, 0

        pairMap := make(map[string]float64)
        pairCount := make(map[string]int)

        type stratStat struct {
                winPnL  float64
                lossPnL float64
                wins    int
                total   int
        }
        stratMap := make(map[string]*stratStat)

        for _, b := range bots {
                strat := string(b.Strategy)
                if _, ok := stratMap[strat]; !ok {
                        stratMap[strat] = &stratStat{}
                }
                for _, t := range b.Trades {
                        if t.Status != botpkg.Closed {
                                continue
                        }
                        allPnL = append(allPnL, t.PnL)
                        totalTrades++
                        pairMap[t.Symbol] += t.PnL
                        pairCount[t.Symbol]++
                        stratMap[strat].total++

                        if t.PnL >= 0 {
                                wins++
                                totalWinPnL += t.PnL
                                stratMap[strat].winPnL += t.PnL
                                stratMap[strat].wins++
                        } else {
                                totalLossPnL += t.PnL
                                stratMap[strat].lossPnL += t.PnL
                        }
                }
        }

        // Win rate
        winRate := 0.0
        if totalTrades > 0 {
                winRate = float64(wins) / float64(totalTrades) * 100
        }

        // Profit factor
        profitFactor := 0.0
        if totalLossPnL != 0 {
                profitFactor = totalWinPnL / math.Abs(totalLossPnL)
        } else if totalWinPnL > 0 {
                profitFactor = 999 // effectively infinite
        }

        // Simplified Sharpe ratio: mean(pnl) / std(pnl) * sqrt(n)
        sharpe := 0.0
        if n := len(allPnL); n > 1 {
                mean := 0.0
                for _, v := range allPnL {
                        mean += v
                }
                mean /= float64(n)
                variance := 0.0
                for _, v := range allPnL {
                        d := v - mean
                        variance += d * d
                }
                std := math.Sqrt(variance / float64(n-1))
                if std > 0 {
                        sharpe = mean / std * math.Sqrt(float64(n))
                }
        }

        // Max drawdown from cumulative PnL curve
        maxDD := 0.0
        peak := 0.0
        cumPnL := 0.0
        for _, v := range allPnL {
                cumPnL += v
                if cumPnL > peak {
                        peak = cumPnL
                }
                if peak > 0 {
                        dd := (peak - cumPnL) / peak * 100
                        if dd > maxDD {
                                maxDD = dd
                        }
                }
        }

        // Current session drawdown from peak equity
        curDD := 0.0
        if peakEquity > 0 && currentEquity < peakEquity {
                curDD = (peakEquity - currentEquity) / peakEquity * 100
        }

        // Build sorted pair list
        type pairEntry struct {
                symbol string
                pnl    float64
                trades int
        }
        var pairs []pairEntry
        for sym, pnl := range pairMap {
                pairs = append(pairs, pairEntry{sym, pnl, pairCount[sym]})
        }
        sort.Slice(pairs, func(i, j int) bool { return pairs[i].pnl > pairs[j].pnl })

        // Build sorted strategy list
        type stratEntry struct {
                strategy string
                pf       float64
                winRate  float64
                trades   int
        }
        var strats []stratEntry
        for name, s := range stratMap {
                pf := 0.0
                if s.lossPnL != 0 {
                        pf = s.winPnL / math.Abs(s.lossPnL)
                } else if s.winPnL > 0 {
                        pf = 999
                }
                wr := 0.0
                if s.total > 0 {
                        wr = float64(s.wins) / float64(s.total) * 100
                }
                strats = append(strats, stratEntry{name, pf, wr, s.total})
        }
        sort.Slice(strats, func(i, j int) bool { return strats[i].pf > strats[j].pf })

        md := metricsData{
                winRate:      winRate,
                profitFactor: profitFactor,
                sharpe:       sharpe,
                maxDrawdown:  maxDD,
                curDrawdown:  curDD,
                totalTrades:  totalTrades,
        }
        for _, p := range pairs {
                md.pairPnL = append(md.pairPnL, struct {
                        symbol string
                        pnl    float64
                        trades int
                }{p.symbol, p.pnl, p.trades})
        }
        for _, s := range strats {
                md.stratPF = append(md.stratPF, struct {
                        strategy string
                        pf       float64
                        winRate  float64
                        trades   int
                }{s.strategy, s.pf, s.winRate, s.trades})
        }
        return md
}

// riskMeterBar draws a horizontal meter bar showing current vs max drawdown.
// Turns red when current drawdown > 70 % of max allowed.
func riskMeterBar(current, maxAllowed float64, width int) string {
        if width < 4 {
                width = 4
        }
        pct := 0.0
        if maxAllowed > 0 {
                pct = current / maxAllowed
                if pct > 1 {
                        pct = 1
                }
        }
        filled := int(pct * float64(width))
        if filled > width {
                filled = width
        }
        alert := pct >= 0.70
        var col lipgloss.Color
        switch {
        case alert:
                col = colorRed
        case pct >= 0.40:
                col = colorYellow
        default:
                col = colorGreen
        }
        bar := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("█", filled)) +
                lipgloss.NewStyle().Foreground(colorDim).Render(strings.Repeat("░", width-filled))
        return bar
}

func (m Model) renderMetrics() string {
        acc := m.currentAccount()
        md := computeMetrics(m.bots, m.peakEquity, acc.Equity)
        cardW := m.width - 6

        // ── Summary stats strip ──────────────────────────────────────────────────
        pfStr := fmt.Sprintf("%.2f", md.profitFactor)
        if md.profitFactor >= 999 {
                pfStr = "∞"
        }
        sharpeStr := fmt.Sprintf("%.2f", md.sharpe)
        sep := mutedStyle.Render("  │  ")

        wr := fmt.Sprintf("%s %s %s",
                mutedStyle.Render("Win Rate"),
                winBar(md.winRate, 8),
                lipgloss.NewStyle().Foreground(colorGreen).Render(fmt.Sprintf("%.1f%%", md.winRate)))
        pf := fmt.Sprintf("%s %s",
                mutedStyle.Render("Profit Factor"),
                lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render(pfStr))
        sh := fmt.Sprintf("%s %s",
                mutedStyle.Render("Sharpe"),
                lipgloss.NewStyle().Foreground(colorViolet).Render(sharpeStr))
        maxDD := fmt.Sprintf("%s %s",
                mutedStyle.Render("Max DD"),
                redStyle.Render(fmt.Sprintf("%.1f%%", md.maxDrawdown)))
        tr := fmt.Sprintf("%s %s",
                mutedStyle.Render("Trades"),
                mutedStyle.Render(fmt.Sprintf("%d", md.totalTrades)))

        summaryCard := cardStyle.Width(cardW).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        titleStyle.Render("  Performance Summary"),
                        " ",
                        lipgloss.JoinHorizontal(lipgloss.Center, wr, sep, pf, sep, sh, sep, maxDD, sep, tr),
                ),
        )

        // ── Risk Meter ───────────────────────────────────────────────────────────
        const maxAllowedDD = 10.0 // 10% max drawdown threshold
        alertStr := ""
        if md.curDrawdown > maxAllowedDD*0.70 {
                alertStr = "  " + redStyle.Render("⚠ ALERT: Drawdown exceeds 70% of limit!")
        }
        meterBar := riskMeterBar(md.curDrawdown, maxAllowedDD, 30)
        meterCard := cardStyle.Width(cardW).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        titleStyle.Render("  Risk Meter — Current Drawdown vs Limit"),
                        " ",
                        fmt.Sprintf("  Current: %s  Max: %.1f%%  [%s] %.1f%%",
                                lipgloss.NewStyle().Foreground(colorYellow).Render(fmt.Sprintf("%.2f%%", md.curDrawdown)),
                                maxAllowedDD,
                                meterBar,
                                md.curDrawdown/maxAllowedDD*100),
                        alertStr,
                ),
        )

        // ── Top performing pairs ─────────────────────────────────────────────────
        pairLines := []string{
                titleStyle.Render("  Top Pairs by Profit"),
                fmt.Sprintf("  %-10s %-8s %s",
                        mutedStyle.Render("Pair"),
                        mutedStyle.Render("Trades"),
                        mutedStyle.Render("P&L (USD)")),
                mutedStyle.Render("  " + strings.Repeat("─", 34)),
        }
        if len(md.pairPnL) == 0 {
                pairLines = append(pairLines, mutedStyle.Render("  No trades yet"))
        }
        for _, p := range md.pairPnL {
                pairLines = append(pairLines,
                        fmt.Sprintf("  %-10s %-8d %s",
                                p.symbol, p.trades,
                                pnlStyle(p.pnl).Render(fmt.Sprintf("%+.2f", p.pnl))))
        }

        // Show regime for each pair
        pairLines = append(pairLines, "", mutedStyle.Render("  Market Regimes (1h cache):"))
        for _, sym := range []string{"EURUSD", "GBPUSD", "USDJPY", "AUDUSD", "USDCAD", "USDCHF", "EURGBP", "EURJPY"} {
                var reg market.Regime
                if m.regime != nil {
                        reg = m.regime.GetCached(sym)
                }
                var regStyle lipgloss.Style
                switch reg {
                case market.RegimeTrending:
                        regStyle = greenStyle
                case market.RegimeVolatile:
                        regStyle = redStyle
                default:
                        regStyle = mutedStyle
                }
                pairLines = append(pairLines,
                        fmt.Sprintf("  %-10s %s", sym, regStyle.Render(reg.String())))
        }

        pairsCard := cardStyle.Width(cardW/2 - 1).Render(strings.Join(pairLines, "\n"))

        // ── Best strategies ──────────────────────────────────────────────────────
        stratLines := []string{
                titleStyle.Render("  Best Strategies by Profit Factor"),
                fmt.Sprintf("  %-20s %-8s %-8s %s",
                        mutedStyle.Render("Strategy"),
                        mutedStyle.Render("PF"),
                        mutedStyle.Render("WinRate"),
                        mutedStyle.Render("Trades")),
                mutedStyle.Render("  " + strings.Repeat("─", 44)),
        }
        if len(md.stratPF) == 0 {
                stratLines = append(stratLines, mutedStyle.Render("  No trades yet"))
        }
        for _, s := range md.stratPF {
                pfLabel := fmt.Sprintf("%.2f", s.pf)
                if s.pf >= 999 {
                        pfLabel = "∞"
                }
                pfCol := pnlStyle(s.pf - 1)
                stratLines = append(stratLines,
                        fmt.Sprintf("  %-20s %-8s %-8s %d",
                                s.strategy,
                                pfCol.Render(pfLabel),
                                greenStyle.Render(fmt.Sprintf("%.0f%%", s.winRate)),
                                s.trades))
        }

        // Strategy weight by regime (EURUSD as reference)
        stratLines = append(stratLines, "", mutedStyle.Render("  Weights by EURUSD Regime:"))
        refRegime := market.RegimeUnknown
        if m.regime != nil {
                refRegime = m.regime.GetCached("EURUSD")
        }
        for _, strat := range []string{"Scalping", "Swing Trading", "Trend Following", "Mean Reversion"} {
                w := market.StrategyWeight(refRegime, strat)
                wBar := winBar(w*100, 6)
                stratLines = append(stratLines,
                        fmt.Sprintf("  %-20s %s %.0f%%", strat, wBar, w*100))
        }

        stratsCard := cardStyle.Width(cardW - cardW/2 - 1).Render(strings.Join(stratLines, "\n"))

        bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, pairsCard, " ", stratsCard)

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        summaryCard, " ", meterCard, " ", bottomRow,
                ),
        )
}

// ─── Settings ─────────────────────────────────────────────────────────────────

func (m Model) renderSettings() string {
        acc := m.currentAccount()
        modeLabel := "DEMO MODE"
        modeDesc := "Trading with virtual $10,000. No real money at risk."
        modeColor := colorYellow
        switchText := "Press [r] to switch to REAL trading"
        if m.useReal {
                modeLabel = "REAL MODE"
                modeDesc = "Trading with real funds. Proceed with caution!"
                modeColor = colorRed
                switchText = "Press [r] to switch back to DEMO mode"
        }

        modeCard := cardStyle.Width(m.width - 4).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        lipgloss.NewStyle().Foreground(modeColor).Bold(true).Render("● "+modeLabel),
                        " ",
                        mutedStyle.Render(modeDesc),
                        " ",
                        yellowStyle.Render(switchText),
                ),
        )

        accountCard := cardStyle.Width(m.width - 4).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        titleStyle.Render("Account Details"),
                        " ",
                        fmt.Sprintf("Name:     %s", lipgloss.NewStyle().Foreground(colorWhite).Render(acc.Name)),
                        fmt.Sprintf("Type:     %s", lipgloss.NewStyle().Foreground(modeColor).Bold(true).Render(string(acc.Type))),
                        fmt.Sprintf("Balance:  %s", lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("$%.2f %s", acc.Balance, acc.Currency))),
                        fmt.Sprintf("Equity:   %s", lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("$%.2f", acc.Equity))),
                ),
        )

        cfg := m.mt5Client.Config
        connStatus := "● Disconnected"
        connColor := colorMuted
        switch m.mt5Status {
        case mt5.StatusConnected:
                connStatus = "● Connected — Live Data Aktif"
                connColor = colorGreen
        case mt5.StatusConnecting:
                connStatus = "◌ Connecting..."
                connColor = colorYellow
        case mt5.StatusHandshake:
                connStatus = "◌ TLS Handshake..."
                connColor = colorYellow
        case mt5.StatusAuthenticating:
                connStatus = "◌ SRP-6a Authenticating..."
                connColor = colorYellow
        case mt5.StatusFailed:
                if !m.mt5NextRetry.IsZero() {
                        secs := int(time.Until(m.mt5NextRetry).Seconds())
                        if secs < 0 {
                                secs = 0
                        }
                        connStatus = fmt.Sprintf("↻ Retry #%d dalam %ds — %s",
                                m.mt5RetryCount+1, secs, m.mt5Err)
                } else {
                        connStatus = "✗ " + m.mt5Err
                }
                connColor = colorRed
        }

        // Build diagnostic log lines
        diagLines := []string{}
        if len(m.mt5Client.Debug) > 0 {
                diagLines = append(diagLines, " ", mutedStyle.Render("── Diagnostic Log ──"))
                for _, line := range m.mt5Client.Debug {
                        diagLines = append(diagLines, mutedStyle.Render("  "+line))
                }
        }

        // Live account block (shown when connected)
        liveLines := []string{}
        if m.mt5Account != nil {
                la := m.mt5Account
                liveLines = append(liveLines,
                        " ",
                        titleStyle.Render("Live Account Data"),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Login:"), lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("%d", la.Login))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Name:"), lipgloss.NewStyle().Foreground(colorWhite).Render(la.Name)),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Balance:"), greenStyle.Render(fmt.Sprintf("%.2f %s", la.Balance, la.Currency))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Equity:"), lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("%.2f %s", la.Equity, la.Currency))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Margin:"), mutedStyle.Render(fmt.Sprintf("%.2f", la.Margin))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Free Margin:"), mutedStyle.Render(fmt.Sprintf("%.2f", la.FreeMargin))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Profit:"), pnlStyle(la.Profit).Render(fmt.Sprintf("%.2f", la.Profit))),
                        fmt.Sprintf("%-12s %s", mutedStyle.Render("Leverage:"), lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("1:%d", la.Leverage))),
                )
        }

        connRows := []string{
                titleStyle.Render("Finex MT5 Connection"),
                " ",
                lipgloss.NewStyle().Foreground(connColor).Bold(true).Render(connStatus),
                " ",
                fmt.Sprintf("%-10s %s", mutedStyle.Render("Company:"), lipgloss.NewStyle().Foreground(colorWhite).Render(cfg.Company)),
                fmt.Sprintf("%-10s %s", mutedStyle.Render("Server:"), lipgloss.NewStyle().Foreground(colorWhite).Render(cfg.Server)),
                fmt.Sprintf("%-10s %s", mutedStyle.Render("Host:"), lipgloss.NewStyle().Foreground(colorMuted).Render(cfg.Host)),
                fmt.Sprintf("%-10s %s", mutedStyle.Render("Login:"), lipgloss.NewStyle().Foreground(colorWhite).Render(cfg.Login)),
                fmt.Sprintf("%-10s %s", mutedStyle.Render("Password:"), lipgloss.NewStyle().Foreground(colorMuted).Render("••••••••")),
        }
        connRows = append(connRows, liveLines...)
        connRows = append(connRows, diagLines...)

        apiCard := cardStyle.Width(m.width - 4).Render(
                lipgloss.JoinVertical(lipgloss.Left, connRows...),
        )

        // Log file status line
        logStatus := mutedStyle.Render("Log: tidak aktif")
        if m.log != nil {
                logStatus = fmt.Sprintf("%s %s",
                        mutedStyle.Render("Log:"),
                        greenStyle.Render(m.log.Path()),
                )
        }

        infoCard := cardStyle.Width(m.width - 4).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        titleStyle.Render("About Finex Bot"),
                        " ",
                        fmt.Sprintf("Version:    %s", lipgloss.NewStyle().Foreground(colorWhite).Render("1.0.0")),
                        fmt.Sprintf("Runtime:    %s", lipgloss.NewStyle().Foreground(colorWhite).Render("Go 1.25")),
                        fmt.Sprintf("Strategies: %s", lipgloss.NewStyle().Foreground(colorWhite).Render("Scalping, Swing, Trend Following, Mean Reversion")),
                        " ",
                        logStatus,
                        mutedStyle.Render("  (trade masuk/keluar, P&L, koneksi MT5 dicatat otomatis)"),
                        " ",
                        mutedStyle.Render("Start in DEMO mode to test strategies risk-free before switching to real trading."),
                ),
        )

        return lipgloss.NewStyle().Padding(1, 2).Render(
                lipgloss.JoinVertical(lipgloss.Left,
                        modeCard, " ", accountCard, " ", apiCard, " ", infoCard,
                ),
        )
}

func (m Model) renderConfirmSwitch() string {
        warning := redStyle.Render("⚠ WARNING: You are about to switch to REAL trading mode.\nThis will use actual funds. Are you sure?")
        if m.useReal {
                warning = yellowStyle.Render("Switch back to DEMO mode?")
        }

        box := cardStyle.Width(60).Render(
                lipgloss.JoinVertical(lipgloss.Center,
                        titleStyle.Render("Confirm Mode Switch"),
                        " ",
                        warning,
                        " ",
                        lipgloss.JoinHorizontal(lipgloss.Center,
                                greenStyle.Render("[y] Confirm"),
                                "   ",
                                redStyle.Render("[n] Cancel"),
                        ),
                ),
        )
        return lipgloss.NewStyle().
                Width(m.width).
                Height(m.height-6).
                Align(lipgloss.Center, lipgloss.Center).
                Render(box)
}

// ─── Bot Form Render ──────────────────────────────────────────────────────────

func (m Model) renderBotForm() string {
        title := "Create New Bot"
        if m.botFormEditing {
                title = "Edit Bot"
        }

        fields := []struct {
                label string
                input textinput.Model
                idx   int
        }{
                {"Bot Name", m.botFormInputs[0], 0},
                {"Risk %", m.botFormInputs[1], 1},
                {"Take Profit %", m.botFormInputs[2], 2},
                {"Stop Loss %", m.botFormInputs[3], 3},
        }

        var lines []string
        lines = append(lines, titleStyle.Render(title), "")

        for _, f := range fields {
                label := mutedStyle.Render(f.label)
                st := inputStyle
                if m.botFormFocused == f.idx {
                        st = focusedInputStyle
                }
                inp := st.Width(40).Render(f.input.View())
                lines = append(lines, label, inp, "")
        }

        // Symbol selector
        symLabel := mutedStyle.Render("Symbol")
        symFocused := m.botFormFocused == len(m.botFormInputs)
        symBorder := colorBorder
        if symFocused {
                symBorder = colorPrimary
        }
        symVal := lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                BorderForeground(symBorder).
                Padding(0, 1).
                Width(40).
                Render(fmt.Sprintf("◀ %-12s ▶  (← →)", symbols[m.botFormSymIdx]))
        lines = append(lines, symLabel, symVal, "")

        // Strategy selector
        strLabel := mutedStyle.Render("Strategy")
        strFocused := m.botFormFocused == len(m.botFormInputs)+1
        strBorder := colorBorder
        if strFocused {
                strBorder = colorPrimary
        }
        strVal := lipgloss.NewStyle().
                Border(lipgloss.RoundedBorder()).
                BorderForeground(strBorder).
                Padding(0, 1).
                Width(40).
                Render(fmt.Sprintf("◀ %-18s ▶  (← →)", string(botpkg.AllStrategies[m.botFormStrIdx])))
        lines = append(lines, strLabel, strVal, "")

        lines = append(lines,
                "",
                lipgloss.JoinHorizontal(lipgloss.Left,
                        greenStyle.Render("[Enter] Save"),
                        "  ",
                        redStyle.Render("[Esc] Cancel"),
                ),
        )

        box := cardStyle.Width(60).Render(strings.Join(lines, "\n"))
        return lipgloss.NewStyle().
                Width(m.width).
                Height(m.height-6).
                Align(lipgloss.Center, lipgloss.Center).
                Render(box)
}

// ─── Help Bar ─────────────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
        // Always-visible navigation hints
        nav := []string{
                keyHint("1-6", "Jump"),
                keyHint("Tab", "Next"),
                keyHint("⇧Tab", "Prev"),
                keyHint("q", "Quit"),
        }

        // Context-specific action hints for the current tab
        var ctx []string
        switch m.activeTab {
        case TabBots:
                ctx = []string{
                        keyHint("↑/↓", "Select"),
                        keyHint("s", "Start/Stop"),
                        keyHint("n", "New"),
                        keyHint("e", "Edit"),
                        keyHint("d", "Delete"),
                }
        case TabSettings:
                ctx = []string{
                        keyHint("r", "Toggle Demo/Real"),
                }
        }

        var parts []string
        parts = append(parts, nav...)
        if len(ctx) > 0 {
                parts = append(parts, mutedStyle.Render("  ·  "))
                parts = append(parts, ctx...)
        }

        return lipgloss.NewStyle().
                Background(colorSurface).
                Foreground(colorMuted).
                Width(m.width).
                Padding(0, 2).
                Render(strings.Join(parts, "  "))
}

// ─── Main ─────────────────────────────────────────────────────────────────────

// calcDailyStats menjumlahkan profit/loss dari semua trade yang ditutup hari ini.
func calcDailyStats(bots []*botpkg.Bot) (profit, loss, winRate float64) {
        today := time.Now()
        wins, losses := 0, 0
        for _, b := range bots {
                for _, t := range b.Trades {
                        if t.Status != botpkg.Closed {
                                continue
                        }
                        if t.ClosedAt.Year() != today.Year() || t.ClosedAt.YearDay() != today.YearDay() {
                                continue
                        }
                        if t.PnL > 0 {
                                profit += t.PnL
                                wins++
                        } else {
                                loss += t.PnL // negatif
                                losses++
                        }
                }
        }
        total := wins + losses
        if total > 0 {
                winRate = float64(wins) / float64(total) * 100
        }
        return
}

// runOptimizer menjalankan Genetic Algorithm optimizer untuk simbol yang diberikan
// pada semua 4 strategy, lalu menyimpan hasilnya ke optimized_params.json.
// Dijalankan via: ./finex-bot --optimize EURUSD
func runOptimizer(symbol string) {
        fmt.Printf("Finex Optimizer — memulai optimasi untuk %s...\n\n", symbol)
        mkt := market.NewMarket()
        candles := mkt.GetHistory(symbol)
        if len(candles) == 0 {
                fmt.Fprintf(os.Stderr, "Error: tidak ada data candle untuk simbol %s\n", symbol)
                os.Exit(1)
        }

        strategies := []string{"Scalping", "Trend Following", "Swing Trading", "Mean Reversion"}
        results := make([]optimizer.OptimizedResult, 0, len(strategies))

        for _, strat := range strategies {
                fmt.Printf("  %-20s ... ", strat)
                result := optimizer.Optimize(symbol, strat, candles)
                results = append(results, result)
                fmt.Printf("fitness=%.4f  wr=%.1f%%  pf=%.2f\n",
                        result.Fitness, result.WinRate, result.ProfitFactor)
                fmt.Printf("    RSI(%d) buy<%.0f sell>%.0f | EMA(%d/%d) | BB(%d, %.1fσ)\n",
                        result.Params.RSIPeriod, result.Params.RSIBuy, result.Params.RSISell,
                        result.Params.EMAFast, result.Params.EMASlow,
                        result.Params.BBPeriod, result.Params.BBMult)
        }

        if err := optimizer.SaveResults(results); err != nil {
                fmt.Fprintf(os.Stderr, "\nError menyimpan hasil: %v\n", err)
                os.Exit(1)
        }
        fmt.Printf("\nHasil disimpan ke %s\n", optimizer.OutputFile)
}

func main() {
        // Parse flags
        dryRun := false
        optimizeSymbol := ""
        args := os.Args[1:]
        for i, arg := range args {
                switch arg {
                case "--dry-run", "-dry-run":
                        dryRun = true
                case "--optimize", "-optimize":
                        if i+1 < len(args) {
                                optimizeSymbol = strings.ToUpper(args[i+1])
                        }
                }
        }

        // Optimizer mode: jalankan GA tanpa TUI lalu exit
        if optimizeSymbol != "" {
                runOptimizer(optimizeSymbol)
                return
        }

        m := initialModel(dryRun)
        p := tea.NewProgram(m,
                tea.WithAltScreen(),
                tea.WithMouseCellMotion(),
        )

        if _, err := p.Run(); err != nil {
                fmt.Fprintf(os.Stderr, "Error running Finex Bot: %v\n", err)
                os.Exit(1)
        }
}
