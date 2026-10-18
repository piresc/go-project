package httptransport

import (
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/asset/usecase"
)

// AssetHandler serves asset endpoints.
type AssetHandler struct {
	uc *usecase.AssetUsecase
}

// NewAssetHandler builds an AssetHandler.
func NewAssetHandler(uc *usecase.AssetUsecase) *AssetHandler {
	return &AssetHandler{uc: uc}
}

// List handles GET /assets. Placeholder: returns an empty list.
func (h *AssetHandler) List(w http.ResponseWriter, _ *http.Request) {
	response.Success(w, http.StatusOK, []any{})
}
