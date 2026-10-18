// Package taskhandler adapts background tasks to price-service use cases.
package taskhandler

import "context"

// Refresher refreshes the cached quotes.
type Refresher interface {
	Refresh(ctx context.Context) error
}

// Refresh returns the handler for the price:refresh task.
func Refresh(uc Refresher) func(ctx context.Context, payload []byte) error {
	return func(ctx context.Context, _ []byte) error {
		return uc.Refresh(ctx)
	}
}
