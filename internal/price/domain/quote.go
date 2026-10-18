// Package domain holds the core price-service entities.
package domain

// Quote is a market quote for one symbol.
type Quote struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	ChangePct float64 `json:"change_pct"`
}
