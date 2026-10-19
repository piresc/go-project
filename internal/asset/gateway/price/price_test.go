package price

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/piresc/gokit/response"

	"github.com/piresc/go-project/pkg/platform"
)

// stubClient is a JSONGetter backed by a canned gokit envelope. Like
// platform.HTTPClient it unwraps the envelope's data before decoding.
type stubClient struct {
	body []byte
	err  error
	path string
}

func (s *stubClient) GetJSON(_ context.Context, endpoint string, out any) error {
	s.path = endpoint
	if s.err != nil {
		return s.err
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(s.body, &env); err != nil {
		return err
	}
	if len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func envelope(t *testing.T, data any) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	response.Success(rec, http.StatusOK, data)
	return rec.Body.Bytes()
}

func TestQuotes_MapsWireToDomain(t *testing.T) {
	stub := &stubClient{body: envelope(t, []map[string]any{
		{"symbol": "AAPL", "price": 190.5, "change_pct": 1.25},
		{"symbol": "MSFT", "price": 410.0, "change_pct": -0.5},
	})}
	got, err := New(stub).Quotes(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if stub.path != "/prices/quotes?symbols=AAPL%2CMSFT" {
		t.Fatalf("endpoint = %q", stub.path)
	}
	if len(got) != 2 || got[0].Symbol != "AAPL" || got[0].Price != 190.5 || got[1].Symbol != "MSFT" || got[1].Price != 410 {
		t.Fatalf("got %+v", got)
	}
}

func TestQuotes_EscapesSymbols(t *testing.T) {
	stub := &stubClient{body: envelope(t, []map[string]any{})}
	got, err := New(stub).Quotes(context.Background(), []string{"BRK.B", "A&B=C"})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want empty non-nil", got, err)
	}
	if stub.path != "/prices/quotes?symbols=BRK.B%2CA%26B%3DC" {
		t.Fatalf("endpoint = %q", stub.path)
	}
}

func TestQuotes_PropagatesError(t *testing.T) {
	_, err := New(&stubClient{err: platform.ErrUnavailable}).Quotes(context.Background(), []string{"AAPL"})
	if !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
