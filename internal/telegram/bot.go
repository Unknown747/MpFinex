package telegram

import (
        "fmt"
        "log"
        "os"
        "strconv"
        "strings"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ─── Command Types ─────────────────────────────────────────────────────────────

type CmdType string

const (
        CmdHelp         CmdType = "help"
        CmdStatus       CmdType = "status"
        CmdBots         CmdType = "bots"
        CmdStart        CmdType = "start"
        CmdStop         CmdType = "stop"
        CmdTrades       CmdType = "trades"
        CmdBalance      CmdType = "balance"
        CmdOptimize     CmdType = "optimize"
        CmdOptimizeMenu CmdType = "optimize_menu"
        CmdToggle       CmdType = "toggle" // Arg = bot index as string
        CmdUnknown      CmdType = "unknown"
)

// BotStatus is a lightweight snapshot passed to keyboard/text builders.
// It avoids importing the bot package (prevents circular imports).
type BotStatus struct {
        Index    int
        Name     string
        Symbol   string
        Strategy string
        Running  bool
        TotalPnL float64
        WinRate  float64
        WinCount int
        LossCount int
}

// Command carries a parsed user action from either a text command or an inline button.
type Command struct {
        Type       CmdType
        Arg        string
        ReplyTo    int64
        MsgID      int    // >0 → edit this message in-place; 0 → send new message
        CallbackID string // non-empty when triggered by an inline button
}

// ─── Bot ───────────────────────────────────────────────────────────────────────

type Bot struct {
        api    *tgbotapi.BotAPI
        chatID int64
        cmdCh  chan Command
}

func FromEnv() (*Bot, error) {
        token := os.Getenv("TELEGRAM_BOT_TOKEN")
        if token == "" {
                return nil, nil
        }
        chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
        if chatIDStr == "" {
                return nil, fmt.Errorf("TELEGRAM_CHAT_ID tidak diset")
        }
        chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
        if err != nil {
                return nil, fmt.Errorf("TELEGRAM_CHAT_ID tidak valid: %w", err)
        }
        api, err := tgbotapi.NewBotAPI(token)
        if err != nil {
                return nil, fmt.Errorf("gagal inisialisasi Telegram bot: %w", err)
        }
        return &Bot{
                api:    api,
                chatID: chatID,
                cmdCh:  make(chan Command, 64),
        }, nil
}

func (b *Bot) Start() { go b.poll() }

func (b *Bot) CmdChan() <-chan Command { return b.cmdCh }

// ─── Send Helpers ──────────────────────────────────────────────────────────────

// Send sends a plain HTML message.
func (b *Bot) Send(text string) {
        msg := tgbotapi.NewMessage(b.chatID, text)
        msg.ParseMode = tgbotapi.ModeHTML
        if _, err := b.api.Send(msg); err != nil {
                log.Printf("[telegram] send error: %v", err)
        }
}

// SendTo sends a plain HTML message to a specific chat (e.g., access-denied reply).
func (b *Bot) SendTo(chatID int64, text string) {
        msg := tgbotapi.NewMessage(chatID, text)
        msg.ParseMode = tgbotapi.ModeHTML
        if _, err := b.api.Send(msg); err != nil {
                log.Printf("[telegram] send error: %v", err)
        }
}

// SendMenu sends an HTML message with an inline keyboard and returns the message ID.
// Use the returned ID later to edit the message in-place.
func (b *Bot) SendMenu(text string, kb tgbotapi.InlineKeyboardMarkup) int {
        msg := tgbotapi.NewMessage(b.chatID, text)
        msg.ParseMode = tgbotapi.ModeHTML
        msg.ReplyMarkup = kb
        sent, err := b.api.Send(msg)
        if err != nil {
                log.Printf("[telegram] send menu error: %v", err)
                return 0
        }
        return sent.MessageID
}

// EditMenu edits an existing message in-place with new text and keyboard.
// Falls back to sending a new message if the edit fails (e.g., content unchanged).
func (b *Bot) EditMenu(msgID int, text string, kb tgbotapi.InlineKeyboardMarkup) {
        if msgID <= 0 {
                b.SendMenu(text, kb)
                return
        }
        edit := tgbotapi.NewEditMessageTextAndMarkup(b.chatID, msgID, text, kb)
        edit.ParseMode = tgbotapi.ModeHTML
        if _, err := b.api.Send(edit); err != nil {
                // Silently ignore "message is not modified" errors — content unchanged is OK
                if !strings.Contains(err.Error(), "message is not modified") {
                        log.Printf("[telegram] edit error: %v", err)
                }
        }
}

// AnswerCallback acknowledges an inline button press (clears the loading spinner).
func (b *Bot) AnswerCallback(callbackID string) {
        if callbackID == "" {
                return
        }
        b.api.Request(tgbotapi.NewCallback(callbackID, "")) //nolint:errcheck
}

// SendOrEdit sends a new message with keyboard (msgID == 0) or edits an
// existing one in-place (msgID > 0). This is the primary display method —
// main.go calls this so it never needs to import tgbotapi directly.
func (b *Bot) SendOrEdit(msgID int, text string, kb tgbotapi.InlineKeyboardMarkup) {
        if msgID > 0 {
                b.EditMenu(msgID, text, kb)
        } else {
                b.SendMenu(text, kb)
        }
}

// SendOrEditAny is like SendOrEdit but accepts interface{} so main.go
// can pass keyboard values without importing tgbotapi.
func (b *Bot) SendOrEditAny(msgID int, text string, kb interface{}) {
        switch k := kb.(type) {
        case tgbotapi.InlineKeyboardMarkup:
                b.SendOrEdit(msgID, text, k)
        default:
                // Fallback: send plain text
                b.Send(text)
        }
}

// ─── Poll ──────────────────────────────────────────────────────────────────────

func (b *Bot) poll() {
        u := tgbotapi.NewUpdate(0)
        u.Timeout = 30
        updates := b.api.GetUpdatesChan(u)

        for update := range updates {
                var (
                        fromChat   int64
                        rawText    string
                        msgID      int
                        callbackID string
                )

                switch {
                case update.Message != nil:
                        fromChat = update.Message.Chat.ID
                        if fromChat != b.chatID {
                                b.SendTo(fromChat, "⛔ Akses ditolak.")
                                continue
                        }
                        rawText = strings.TrimSpace(update.Message.Text)
                        msgID = 0 // text commands always send new message
                        if !strings.HasPrefix(rawText, "/") {
                                continue
                        }
                        rawText = rawText[1:] // strip leading "/"

                case update.CallbackQuery != nil:
                        cb := update.CallbackQuery
                        if cb.Message == nil {
                                continue
                        }
                        fromChat = cb.Message.Chat.ID
                        if fromChat != b.chatID {
                                continue
                        }
                        rawText = cb.Data
                        msgID = cb.Message.MessageID
                        callbackID = cb.ID
                        // Immediately answer callback to clear loading spinner
                        b.api.Request(tgbotapi.NewCallback(callbackID, "")) //nolint:errcheck

                default:
                        continue
                }

                // Parse "command arg1 arg2..."
                parts := strings.Fields(rawText)
                if len(parts) == 0 {
                        continue
                }
                rawCmd := strings.ToLower(strings.SplitN(parts[0], "@", 2)[0])
                arg := ""
                if len(parts) > 1 {
                        arg = strings.Join(parts[1:], " ")
                }

                cmd := Command{
                        ReplyTo:    fromChat,
                        Arg:        arg,
                        MsgID:      msgID,
                        CallbackID: callbackID,
                }

                switch rawCmd {
                case "help", "start", "menu":
                        cmd.Type = CmdHelp
                case "status":
                        cmd.Type = CmdStatus
                case "bots", "kelola":
                        cmd.Type = CmdBots
                case "mulai", "startbot":
                        cmd.Type = CmdStart
                case "henti", "stopbot":
                        cmd.Type = CmdStop
                case "trades", "posisi":
                        cmd.Type = CmdTrades
                case "balance", "saldo":
                        cmd.Type = CmdBalance
                case "optimize", "optimasi":
                        cmd.Type = CmdOptimize
                case "optimize_menu":
                        cmd.Type = CmdOptimizeMenu
                case "toggle":
                        cmd.Type = CmdToggle
                default:
                        cmd.Type = CmdUnknown
                }

                select {
                case b.cmdCh <- cmd:
                default:
                }
        }
}

// ─── Keyboard Generators ───────────────────────────────────────────────────────

// MainMenuKeyboard returns the main dashboard inline keyboard.
func MainMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
        return tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("▶️ Start All", "startbot"),
                        tgbotapi.NewInlineKeyboardButtonData("⏹ Stop All", "stopbot"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("📊 Status", "status"),
                        tgbotapi.NewInlineKeyboardButtonData("💰 Balance", "balance"),
                        tgbotapi.NewInlineKeyboardButtonData("📈 Trades", "trades"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🤖 Kelola Bot", "bots"),
                        tgbotapi.NewInlineKeyboardButtonData("⚙️ Optimize", "optimize_menu"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "menu"),
                ),
        )
}

// BackMenuKeyboard returns a single row with Refresh + Menu buttons.
func BackMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
        return tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "status"),
                        tgbotapi.NewInlineKeyboardButtonData("🏠 Menu", "menu"),
                ),
        )
}

// BotListKeyboard builds a keyboard with one toggle-button per bot plus control rows.
func BotListKeyboard(bots []BotStatus) tgbotapi.InlineKeyboardMarkup {
        var rows [][]tgbotapi.InlineKeyboardButton
        for _, b := range bots {
                icon := "🔴"
                action := "▶️ Start"
                if b.Running {
                        icon = "🟢"
                        action = "⏹ Stop"
                }
                label := fmt.Sprintf("%s %s · %s", icon, b.Name, action)
                data := fmt.Sprintf("toggle %d", b.Index)
                rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData(label, data),
                ))
        }
        rows = append(rows,
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("▶️ Start All", "startbot"),
                        tgbotapi.NewInlineKeyboardButtonData("⏹ Stop All", "stopbot"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "bots"),
                        tgbotapi.NewInlineKeyboardButtonData("🏠 Menu", "menu"),
                ),
        )
        return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// OptimizeKeyboard returns a symbol-picker keyboard for the optimizer.
func OptimizeKeyboard() tgbotapi.InlineKeyboardMarkup {
        return tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("EURUSD", "optimize EURUSD"),
                        tgbotapi.NewInlineKeyboardButtonData("GBPUSD", "optimize GBPUSD"),
                        tgbotapi.NewInlineKeyboardButtonData("USDJPY", "optimize USDJPY"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("AUDUSD", "optimize AUDUSD"),
                        tgbotapi.NewInlineKeyboardButtonData("USDCAD", "optimize USDCAD"),
                        tgbotapi.NewInlineKeyboardButtonData("USDCHF", "optimize USDCHF"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("EURGBP", "optimize EURGBP"),
                        tgbotapi.NewInlineKeyboardButtonData("EURJPY", "optimize EURJPY"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🏠 Menu", "menu"),
                ),
        )
}

// TradesBackKeyboard returns a simple back keyboard for trade replies.
func TradesBackKeyboard() tgbotapi.InlineKeyboardMarkup {
        return tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🔄 Refresh", "trades"),
                        tgbotapi.NewInlineKeyboardButtonData("🏠 Menu", "menu"),
                ),
        )
}

// ─── Message Formatters ────────────────────────────────────────────────────────

const divider = "━━━━━━━━━━━━━━━━━━━━━━"

// DashboardText builds the main dashboard message.
func DashboardText(mode, mt5Status string, balance, equity float64, running, total int, session string) string {
        mt5Icon := "✅"
        if strings.Contains(strings.ToLower(mt5Status), "fail") ||
                strings.Contains(strings.ToLower(mt5Status), "disconnect") {
                mt5Icon = "❌"
        }
        sessionIcon := "🌙"
        if session != "Asia" {
                sessionIcon = "🌍"
        }
        botIcon := "🔴"
        if running > 0 {
                botIcon = "🟢"
        }
        return fmt.Sprintf(
                "🤖 <b>FINEX TRADING BOT</b>\n"+
                        divider+"\n"+
                        "📡 MT5: %s %s  |  🔧 Mode: <b>%s</b>\n"+
                        "%s Sesi: <b>%s</b>\n"+
                        divider+"\n"+
                        "💰 Balance  <code>$%10.2f</code>\n"+
                        "💎 Equity   <code>$%10.2f</code>\n"+
                        divider+"\n"+
                        "%s Bot Aktif  <b>%d / %d</b>\n"+
                        divider+"\n"+
                        "<i>Pilih aksi:</i>",
                mt5Icon, mt5Status, mode,
                sessionIcon, session,
                balance, equity,
                botIcon, running, total,
        )
}

// StatusText builds the status detail message.
func StatusText(mode, mt5Status string, balance, equity, drawdown float64, running, total int, session string) string {
        mt5Icon := "✅"
        if strings.Contains(strings.ToLower(mt5Status), "fail") ||
                strings.Contains(strings.ToLower(mt5Status), "disconnect") {
                mt5Icon = "❌"
        }
        ddColor := "🟢"
        if drawdown >= 5 {
                ddColor = "🔴"
        } else if drawdown >= 2 {
                ddColor = "🟡"
        }
        return fmt.Sprintf(
                "📊 <b>STATUS AKUN</b>\n"+
                        divider+"\n"+
                        "📡 MT5  : %s %s\n"+
                        "🔧 Mode : <b>%s</b>\n"+
                        "🌍 Sesi : <b>%s</b>\n"+
                        divider+"\n"+
                        "💰 Balance  : <code>$%.2f</code>\n"+
                        "💎 Equity   : <code>$%.2f</code>\n"+
                        "%s Drawdown : <code>%.2f%%</code>\n"+
                        divider+"\n"+
                        "🤖 Bot Aktif : <b>%d / %d</b>",
                mt5Icon, mt5Status,
                mode,
                session,
                balance, equity,
                ddColor, drawdown,
                running, total,
        )
}

// BalanceText builds the balance message.
func BalanceText(mode string, balance, equity float64) string {
        pnl := equity - balance
        pnlIcon := "🟢"
        if pnl < 0 {
                pnlIcon = "🔴"
        }
        return fmt.Sprintf(
                "💰 <b>SALDO AKUN</b>  (<code>%s</code>)\n"+
                        divider+"\n"+
                        "💵 Balance : <code>$%.2f</code>\n"+
                        "💎 Equity  : <code>$%.2f</code>\n"+
                        "%s Open P&amp;L : <code>$%+.2f</code>",
                mode, balance, equity, pnlIcon, pnl,
        )
}

// BotListText builds the bot management message.
func BotListText(bots []BotStatus) string {
        if len(bots) == 0 {
                return "🤖 <b>KELOLA BOT</b>\n" + divider + "\nBelum ada bot terdaftar."
        }
        text := "🤖 <b>KELOLA BOT</b>\n" + divider + "\n"
        for i, b := range bots {
                icon := "🔴"
                statusStr := "Berhenti"
                if b.Running {
                        icon = "🟢"
                        statusStr = "Aktif"
                }
                pnlIcon := "▪️"
                if b.TotalPnL > 0 {
                        pnlIcon = "🟢"
                } else if b.TotalPnL < 0 {
                        pnlIcon = "🔴"
                }
                text += fmt.Sprintf(
                        "<b>%d. %s %s</b>\n"+
                                "   📌 %s  ·  %s\n"+
                                "   📊 Status  : %s\n"+
                                "   %s P&amp;L    : <code>$%+.2f</code>  ·  W:%d / L:%d\n",
                        i+1, icon, b.Name,
                        b.Symbol, b.Strategy,
                        statusStr,
                        pnlIcon, b.TotalPnL, b.WinCount, b.LossCount,
                )
                if i < len(bots)-1 {
                        text += "──────────────────────\n"
                }
        }
        return text
}

// OptimizeMenuText builds the optimize symbol-picker message.
func OptimizeMenuText() string {
        return "⚙️ <b>GENETIC ALGORITHM OPTIMIZER</b>\n" +
                divider + "\n" +
                "Pilih simbol untuk dioptimalkan.\n\n" +
                "🔬 GA menjalankan <b>4 strategi</b> sekaligus:\n" +
                "   · Scalping  · Trend Following\n" +
                "   · Swing Trading  · Mean Reversion\n\n" +
                "⏱ Estimasi: <b>1–2 menit</b> per simbol\n" +
                "📩 Hasil dikirim otomatis ke chat ini.\n" +
                divider + "\n" +
                "<i>Pilih simbol:</i>"
}

// ─── Misc Helpers ──────────────────────────────────────────────────────────────

// FormatDuration formats a duration as "Xj Ym" or "Ym".
func FormatDuration(d time.Duration) string {
        d = d.Round(time.Minute)
        h := int(d.Hours())
        m := int(d.Minutes()) % 60
        if h > 0 {
                return fmt.Sprintf("%dj %dm", h, m)
        }
        return fmt.Sprintf("%dm", m)
}
