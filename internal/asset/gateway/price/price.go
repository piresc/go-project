// Package price holds the adapter for price-service's quotes API.
package price

import (
	"context"
	"net/url"
	"strings"

	"github.com/piresc/go-project/internal/asset/domain"
)

const quotesEndpoint = "/prices/quotes"

// JSONGetter is the HTTP client the adapter needs. *platform.HTTPClient
// satisfies it.
type JSONGetter interface {
	GetJSON(ctx context.Context, endpoint string, out any) error
}

// Client fetches quotes from price-service.
type Client struct {
	http JSONGetter
}

// New builds a price-service Client.
func New(http JSONGetter) *Client {
	return &Client{http: http}
}

type quoteWire struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
}

// Quotes returns the quotes for symbols. Unknown symbols are omitted. While
// the client's breaker is open the error wraps platform.ErrUnavailable.
func (c *Client) Quotes(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	endpoint := quotesEndpoint + "?symbols=" + url.QueryEscape(strings.Join(symbols, ","))
	var wire []quoteWire
	if err := c.http.GetJSON(ctx, endpoint, &wire); err != nil {
		return nil, err
	}
	quotes := make([]domain.Quote, 0, len(wire))
	for _, q := range wire {
		quotes = append(quotes, domain.Quote{Symbol: q.Symbol, Price: q.Price})
	}
	return quotes, nil
}
