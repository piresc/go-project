// Package market holds the market-data adapter.
package market

import (
	"context"

	"github.com/piresc/go-project/internal/price/domain"
)

const quotesEndpoint = "/v1/quotes"

// JSONGetter is the HTTP client the adapter needs. *platform.HTTPClient
// satisfies it.
type JSONGetter interface {
	GetJSON(ctx context.Context, endpoint string, out any) error
}

// Client fetches quotes from the market-data API.
type Client struct {
	http JSONGetter
}

// New builds a market-data Client.
func New(http JSONGetter) *Client {
	return &Client{http: http}
}

type quotesResponse struct {
	Quotes []struct {
		Symbol    string  `json:"symbol"`
		Price     float64 `json:"price"`
		ChangePct float64 `json:"change_pct"`
	} `json:"quotes"`
}

// Quotes returns the current quotes. While the client's breaker is open the
// error wraps platform.ErrUnavailable.
func (c *Client) Quotes(ctx context.Context) ([]domain.Quote, error) {
	var resp quotesResponse
	if err := c.http.GetJSON(ctx, quotesEndpoint, &resp); err != nil {
		return nil, err
	}
	quotes := make([]domain.Quote, 0, len(resp.Quotes))
	for _, q := range resp.Quotes {
		quotes = append(quotes, domain.Quote{Symbol: q.Symbol, Price: q.Price, ChangePct: q.ChangePct})
	}
	return quotes, nil
}
