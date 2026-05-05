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

type CmdType string

const (
        CmdHelp     CmdType = "help"
        CmdStatus   CmdType = "status"
        CmdBots     CmdType = "bots"
        CmdStart    CmdType = "start"
        CmdStop     CmdType = "stop"
        CmdTrades   CmdType = "trades"
        CmdBalance  CmdType = "balance"
        CmdOptimize CmdType = "optimize"
        CmdUnknown  CmdType = "unknown"
)

type Command struct {
        Type    CmdType
        Arg     string
        ReplyTo int64
}

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
                cmdCh:  make(chan Command, 32),
        }, nil
}

func (b *Bot) Start() {
        go b.poll()
}

func (b *Bot) CmdChan() <-chan Command {
        return b.cmdCh
}

func (b *Bot) Send(text string) {
        msg := tgbotapi.NewMessage(b.chatID, text)
        msg.ParseMode = tgbotapi.ModeHTML
        if _, err := b.api.Send(msg); err != nil {
                log.Printf("[telegram] send error: %v", err)
        }
}

func (b *Bot) SendTo(chatID int64, text string) {
        msg := tgbotapi.NewMessage(chatID, text)
        msg.ParseMode = tgbotapi.ModeHTML
        if _, err := b.api.Send(msg); err != nil {
                log.Printf("[telegram] send error: %v", err)
        }
}

func (b *Bot) poll() {
        u := tgbotapi.NewUpdate(0)
        u.Timeout = 30
        updates := b.api.GetUpdatesChan(u)

        for update := range updates {
                if update.Message == nil {
                        continue
                }

                fromChat := update.Message.Chat.ID
                if fromChat != b.chatID {
                        b.SendTo(fromChat, "⛔ Akses ditolak.")
                        continue
                }

                text := strings.TrimSpace(update.Message.Text)
                if !strings.HasPrefix(text, "/") {
                        continue
                }

                parts := strings.Fields(text)
                rawCmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
                rawCmd = strings.SplitN(rawCmd, "@", 2)[0]

                arg := ""
                if len(parts) > 1 {
                        arg = strings.Join(parts[1:], " ")
                }

                cmd := Command{ReplyTo: fromChat, Arg: arg}
                switch rawCmd {
                case "help", "start":
                        cmd.Type = CmdHelp
                case "status":
                        cmd.Type = CmdStatus
                case "bots":
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
                default:
                        cmd.Type = CmdUnknown
                }

                select {
                case b.cmdCh <- cmd:
                default:
                }
        }
}

func HelpText() string {
        return `🤖 <b>Finex CLI — Telegram Bot</b>

<b>Perintah yang tersedia:</b>
/status — Status koneksi MT5 &amp; ringkasan akun
/bots — Daftar semua bot dan statusnya
/startbot — Mulai <b>semua</b> bot sekaligus (auto trade)
/startbot &lt;nama&gt; — Mulai satu bot berdasarkan nama
/stopbot — Hentikan <b>semua</b> bot sekaligus
/stopbot &lt;nama&gt; — Hentikan satu bot berdasarkan nama
/trades — Posisi yang sedang terbuka
/balance — Saldo akun saat ini
/optimize &lt;SYMBOL&gt; — Jalankan optimizer GA untuk simbol (contoh: /optimize EURUSD)
/help — Tampilkan menu ini`
}

func FormatDuration(d time.Duration) string {
        d = d.Round(time.Minute)
        h := int(d.Hours())
        m := int(d.Minutes()) % 60
        if h > 0 {
                return fmt.Sprintf("%dj %dm", h, m)
        }
        return fmt.Sprintf("%dm", m)
}
