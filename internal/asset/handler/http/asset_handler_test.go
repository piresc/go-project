package httptransport

import (
	"context"
	"encoding/json"
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
		wantIDs  []string
	}{
		{"success", fakeAssets{assets: []domain.Asset{{ID: "a1"}}}, http.StatusOK, []string{"a1"}},
		{"empty", fakeAssets{}, http.StatusOK, []string{}},
		{"error", fakeAssets{err: errors.New("boom")}, http.StatusInternalServerError, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewAssetHandler(tt.svc, slog.New(slog.NewTextHandler(io.Discard, nil))).List(rec, httptest.NewRequest(http.MethodGet, "/assets", nil))
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantCode, rec.Body)
			}
			if tt.wantIDs == nil {
				return
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if len(tt.wantIDs) == 0 {
				// An empty list is left out of the envelope.
				if _, ok := raw["data"]; ok {
					t.Fatalf("empty list: data key present; body=%s", rec.Body)
				}
				if string(raw["success"]) != "true" {
					t.Fatalf("success = %s; body=%s", raw["success"], rec.Body)
				}
				return
			}
			var env struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if len(env.Data) != len(tt.wantIDs) {
				t.Fatalf("data = %v, want %d items; body=%s", env.Data, len(tt.wantIDs), rec.Body)
			}
			for i, id := range tt.wantIDs {
				if env.Data[i].ID != id {
					t.Fatalf("data[%d].id = %q, want %q", i, env.Data[i].ID, id)
				}
			}
		})
	}
}
