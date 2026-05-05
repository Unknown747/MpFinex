// Package logger provides thread-safe structured file logging for Finex Bot.
// All trading activity, MT5 connection events, and errors are written to
// finex-bot.log in append mode so sessions accumulate over time.
package logger

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	kindTradeOpen  = "TRADE_OPEN "
	kindTradeClose = "TRADE_CLOSE"
	kindBotStart   = "BOT_START  "
	kindBotStop    = "BOT_STOP   "
	kindMT5Conn    = "MT5_CONNECT"
	kindMT5Auth    = "MT5_AUTH   "
	kindMT5Disc    = "MT5_DISCONN"
	kindMT5Error   = "MT5_ERROR  "
	kindSession    = "SESSION    "
	kindError      = "ERROR      "
)

// Logger writes structured log lines to a file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// New opens (or creates) the log file at path in append mode.
func New(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f, path: path}, nil
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file.Sync()
	l.file.Close()
}

// Path returns the log file path.
func (l *Logger) Path() string { return l.path }

func (l *Logger) write(kind, msg string) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s | %s | %s\n", ts, kind, msg)
	l.mu.Lock()
	l.file.WriteString(line)
	l.mu.Unlock()
}

// ─── Session ──────────────────────────────────────────────────────────────────

// SessionStart writes a session header so log files are easy to segment.
func (l *Logger) SessionStart(version, mode string) {
	l.write(kindSession, fmt.Sprintf("START version=%s mode=%s pid=%d",
		version, mode, os.Getpid()))
	l.write(kindSession, "────────────────────────────────────────────────────")
}

// SessionEnd writes a session footer with a brief P&L summary.
func (l *Logger) SessionEnd(totalPnL float64, totalTrades int) {
	l.write(kindSession, "────────────────────────────────────────────────────")
	l.write(kindSession, fmt.Sprintf("END totalPnL=%+.2f trades=%d",
		totalPnL, totalTrades))
}

// ─── Bot events ───────────────────────────────────────────────────────────────

// BotStart logs when a bot is toggled on.
func (l *Logger) BotStart(botID int, name, symbol, strategy string) {
	l.write(kindBotStart, fmt.Sprintf("bot=%d name=%q symbol=%s strategy=%s",
		botID, name, symbol, strategy))
}

// BotStop logs when a bot is toggled off.
func (l *Logger) BotStop(botID int, name string, totalPnL float64, wins, losses int) {
	winRate := 0.0
	if wins+losses > 0 {
		winRate = float64(wins) / float64(wins+losses) * 100
	}
	l.write(kindBotStop, fmt.Sprintf(
		"bot=%d name=%q totalPnL=%+.2f wins=%d losses=%d winRate=%.0f%%",
		botID, name, totalPnL, wins, losses, winRate))
}

// ─── Trade events ─────────────────────────────────────────────────────────────

// TradeOpen logs a trade entry.
func (l *Logger) TradeOpen(
	botID int, botName, symbol, side string,
	tradeID int, entryPrice, qty float64,
) {
	l.write(kindTradeOpen, fmt.Sprintf(
		"bot=%d name=%q trade=%d symbol=%s side=%s entry=%.5f qty=%.4f",
		botID, botName, tradeID, symbol, side, entryPrice, qty))
}

// TradeClose logs a trade exit with P&L and duration.
func (l *Logger) TradeClose(
	botID int, botName, symbol, side string,
	tradeID int, entryPrice, exitPrice, pnl, qty float64,
	openedAt, closedAt time.Time,
) {
	dur := closedAt.Sub(openedAt).Round(time.Second)
	outcome := "WIN "
	if pnl < 0 {
		outcome = "LOSS"
	}
	l.write(kindTradeClose, fmt.Sprintf(
		"bot=%d name=%q trade=%d symbol=%s side=%s entry=%.5f exit=%.5f qty=%.4f pnl=%+.2f outcome=%s dur=%s",
		botID, botName, tradeID, symbol, side,
		entryPrice, exitPrice, qty, pnl, outcome, dur))
}

// ─── MT5 events ───────────────────────────────────────────────────────────────

// MT5Connecting logs the start of a connection attempt.
func (l *Logger) MT5Connecting(host, login string) {
	l.write(kindMT5Conn, fmt.Sprintf("host=%s login=%s", host, login))
}

// MT5Authenticated logs a successful SRP-6a authentication with account data.
func (l *Logger) MT5Authenticated(login int64, balance float64, currency string) {
	l.write(kindMT5Auth, fmt.Sprintf(
		"login=%d balance=%.2f currency=%s", login, balance, currency))
}

// MT5Disconnected logs when the connection drops.
func (l *Logger) MT5Disconnected(reason string) {
	l.write(kindMT5Disc, fmt.Sprintf("reason=%q", reason))
}

// MT5Error logs a connection or protocol error.
func (l *Logger) MT5Error(err string) {
	l.write(kindMT5Error, fmt.Sprintf("err=%q", err))
}

// ─── Generic error ────────────────────────────────────────────────────────────

// Error logs a generic runtime error.
func (l *Logger) Error(context, msg string) {
	l.write(kindError, fmt.Sprintf("ctx=%q msg=%q", context, msg))
}
