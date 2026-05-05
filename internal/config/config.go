// Package config handles persistent storage of bot configurations.
// Bots are saved to bots.json in the working directory so they
// survive application restarts.
package config

import (
	"encoding/json"
	"os"

	"github.com/finex/finex-cli/internal/bot"
)

const botsFile = "bots.json"

// BotConfig is the serialisable form of a bot (runtime state excluded).
type BotConfig struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	Symbol        string       `json:"symbol"`
	Strategy      bot.Strategy `json:"strategy"`
	RiskPct       float64      `json:"risk_pct"`
	TakeProfitPct float64      `json:"take_profit_pct"`
	StopLossPct   float64      `json:"stop_loss_pct"`
}

// SaveBots writes the current bot list to bots.json.
func SaveBots(bots []*bot.Bot) error {
	cfgs := make([]BotConfig, 0, len(bots))
	for _, b := range bots {
		cfgs = append(cfgs, BotConfig{
			ID:            b.ID,
			Name:          b.Name,
			Symbol:        b.Symbol,
			Strategy:      b.Strategy,
			RiskPct:       b.RiskPct,
			TakeProfitPct: b.TakeProfitPct,
			StopLossPct:   b.StopLossPct,
		})
	}
	data, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(botsFile, data, 0644)
}

// LoadBots reads bots.json and returns the saved bots.
// Returns nil, nil when the file does not exist yet (first run).
func LoadBots() ([]*bot.Bot, error) {
	data, err := os.ReadFile(botsFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfgs []BotConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return nil, err
	}
	bots := make([]*bot.Bot, 0, len(cfgs))
	for _, c := range cfgs {
		bots = append(bots, bot.NewBot(
			c.ID, c.Name, c.Symbol, c.Strategy,
			c.RiskPct, c.TakeProfitPct, c.StopLossPct,
		))
	}
	return bots, nil
}
