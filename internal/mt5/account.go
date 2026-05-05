package mt5

import (
	"math"
)

// AccountInfo holds live account data received from the MT5 trade server.
type AccountInfo struct {
	Login      int64
	Name       string
	Company    string
	Server     string
	Currency   string
	Balance    float64
	Equity     float64
	Margin     float64
	FreeMargin float64
	Profit     float64
	Leverage   int
	Live       bool // true = data from real server, not simulated
}

// parseAccountBody attempts to decode the account info binary payload.
//
// Expected layout (little-endian):
//
//	[int64   login      ]
//	[float64 balance    ]
//	[float64 equity     ]
//	[float64 margin     ]
//	[float64 free_margin]
//	[float64 profit     ]
//	[uint32  leverage   ]
//	[cstr    currency   ]
//	[cstr    name       ]
//
// Returns nil if body is too short or values look corrupt, so the caller can
// gracefully degrade rather than show garbage data.
func parseAccountBody(body []byte) *AccountInfo {
	if len(body) < 52 {
		return nil
	}
	info := &AccountInfo{Live: true}
	off := 0

	info.Login, off = getInt64LE(body, off)
	info.Balance, off = getFloat64LE(body, off)
	info.Equity, off = getFloat64LE(body, off)
	info.Margin, off = getFloat64LE(body, off)
	info.FreeMargin, off = getFloat64LE(body, off)
	info.Profit, off = getFloat64LE(body, off)

	// Sanity guard — corrupt data has NaN / Inf / absurd values
	if math.IsNaN(info.Balance) || math.IsInf(info.Balance, 0) ||
		math.Abs(info.Balance) > 1e12 {
		return nil
	}

	var lev uint32
	lev, off = getUint32LE(body, off)
	info.Leverage = int(lev)

	if off < len(body) {
		info.Currency, off = getString(body, off)
	}
	if off < len(body) {
		info.Name, off = getString(body, off)
	}
	_ = off
	return info
}

// parseAccountBodySkip32 tries the same layout but skips the first 32 bytes
// (some builds prepend M2 proof before account fields).
func parseAccountBodySkip32(body []byte) *AccountInfo {
	if len(body) < 32+52 {
		return nil
	}
	return parseAccountBody(body[32:])
}
