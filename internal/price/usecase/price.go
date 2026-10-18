// Package usecase holds price-service business logic.
package usecase

// PriceUsecase orchestrates price operations. Placeholder: no logic yet.
type PriceUsecase struct {
	quotes QuoteSource
}

// NewPriceUsecase builds a PriceUsecase on top of its quote source port.
func NewPriceUsecase(quotes QuoteSource) *PriceUsecase {
	return &PriceUsecase{quotes: quotes}
}
