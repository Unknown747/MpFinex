package account

import (
	"time"
)

type Type string

const (
	Demo Type = "DEMO"
	Real Type = "REAL"
)

type Account struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Type      Type      `json:"type"`
	Balance   float64   `json:"balance"`
	Equity    float64   `json:"equity"`
	Currency  string    `json:"currency"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

func NewDemoAccount() *Account {
	return &Account{
		ID:        1,
		Name:      "Demo Account",
		Type:      Demo,
		Balance:   10000.00,
		Equity:    10000.00,
		Currency:  "USD",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
}

func NewRealAccount() *Account {
	return &Account{
		ID:        2,
		Name:      "Real Account",
		Type:      Real,
		Balance:   0.00,
		Equity:    0.00,
		Currency:  "USD",
		IsActive:  false,
		CreatedAt: time.Now(),
	}
}
