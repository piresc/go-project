package domain

import "time"

// Portfolio is a user's collection of holdings. One exists per user.
type Portfolio struct {
	UserID    string    `db:"user_id"`
	CreatedAt time.Time `db:"created_at"`
}

// Holding is a quantity of one symbol held in a portfolio.
type Holding struct {
	Symbol   string  `db:"symbol"`
	Quantity float64 `db:"quantity"`
}

// Quote is asset-service's view of a price-service quote.
type Quote struct {
	Symbol string
	Price  float64
}

// HoldingValue is a holding priced at the current quote.
type HoldingValue struct {
	Symbol   string
	Quantity float64
	Price    float64
	Value    float64
}

// Valuation is a portfolio priced at current quotes. Holdings without a quote
// are listed in Unpriced and excluded from Total.
type Valuation struct {
	Holdings []HoldingValue
	Total    float64
	Unpriced []string
}
