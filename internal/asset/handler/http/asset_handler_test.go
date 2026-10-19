package httptransport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/piresc/go-project/internal/asset/domain"
)

type fakeAssets struct {
	assets []domain.Asset
	err    error
}

func (f fakeAssets) List(context.Context) ([]domain.Asset, error) { return f.assets, f.err }

func TestAssetHandler_List(t *testing.T) {
	tests := []struct {
		name     string
		svc      fakeAssets
		wantCode int
	}{
		{"success", fakeAssets{assets: []domain.Asset{{ID: "a1"}}}, http.StatusOK},
		{"error", fakeAssets{err: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewAssetHandler(tt.svc, slog.New(slog.NewTextHandler(io.Discard, nil))).List(rec, httptest.NewRequest(http.MethodGet, "/assets", nil))
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantCode, rec.Body)
			}
		})
	}
}
