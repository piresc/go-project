package httptransport

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/internal/asset/domain"
)

// AssetService captures the asset operations the handler serves.
type AssetService interface {
	List(ctx context.Context) ([]domain.Asset, error)
}

// AssetHandler serves asset endpoints.
type AssetHandler struct {
	svc AssetService
	log *slog.Logger
}

// NewAssetHandler builds an AssetHandler.
func NewAssetHandler(svc AssetService, log *slog.Logger) *AssetHandler {
	return &AssetHandler{svc: svc, log: log}
}

type assetResponse struct {
	ID string `json:"id"`
}

// List handles GET /assets.
func (h *AssetHandler) List(w http.ResponseWriter, r *http.Request) {
	assets, err := h.svc.List(r.Context())
	if err != nil {
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		response.Error(w, http.StatusInternalServerError, "internal server error")
		return
	}
	out := make([]assetResponse, 0, len(assets))
	for _, a := range assets {
		out = append(out, assetResponse{ID: a.ID})
	}
	response.Success(w, http.StatusOK, out)
}
