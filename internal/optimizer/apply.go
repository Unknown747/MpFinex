package optimizer

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/finex/finex-cli/internal/bot"
)

// ApplyToBots reads optimized_params.json (written by Optimize + SaveResults)
// and sets indicator parameters on each bot whose symbol+strategy key matches.
// Silent no-op if the file does not exist yet — bots keep their strategy defaults.
func ApplyToBots(bots []*bot.Bot) int {
	data, err := os.ReadFile(OutputFile)
	if err != nil {
		return 0
	}
	var all AllResults
	if err := json.Unmarshal(data, &all); err != nil {
		return 0
	}
	applied := 0
	for _, b := range bots {
		key := fmt.Sprintf("%s_%s", b.Symbol, b.Strategy)
		r, ok := all[key]
		if !ok {
			continue
		}
		b.RSIPeriod = r.Params.RSIPeriod
		b.RSIBuy = r.Params.RSIBuy
		b.RSISell = r.Params.RSISell
		b.EMAFast = r.Params.EMAFast
		b.EMASlow = r.Params.EMASlow
		b.BBPeriod = r.Params.BBPeriod
		b.BBMult = r.Params.BBMult
		applied++
	}
	return applied
}
