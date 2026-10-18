package httptransport

import (
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/services/price-service/internal/usecase"
)

// PriceHandler serves price endpoints.
type PriceHandler struct {
	uc *usecase.PriceUsecase
}

// NewPriceHandler builds a PriceHandler.
func NewPriceHandler(uc *usecase.PriceUsecase) *PriceHandler {
	return &PriceHandler{uc: uc}
}

// TopPerformers handles GET /prices/top-performers. Placeholder: returns an empty list.
func (h *PriceHandler) TopPerformers(w http.ResponseWriter, _ *http.Request) {
	response.Success(w, http.StatusOK, []any{})
}
