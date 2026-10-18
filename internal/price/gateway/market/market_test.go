package market

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/piresc/go-project/pkg/platform"
)

// stubClient is a JSONGetter backed by a canned response.
type stubClient struct {
	body string
	err  error
	path string
}

func (s *stubClient) GetJSON(_ context.Context, endpoint string, out any) error {
	s.path = endpoint
	if s.err != nil {
		return s.err
	}
	return json.Unmarshal([]byte(s.body), out)
}

func TestQuotes_MapsWireToDomain(t *testing.T) {
	stub := &stubClient{body: `{"quotes":[{"symbol":"AAPL","price":190.5,"change_pct":1.25},{"symbol":"MSFT","price":410,"change_pct":-0.5}]}`}
	got, err := New(stub).Quotes(context.Background())
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if stub.path != "/v1/quotes" {
		t.Fatalf("endpoint = %q, want /v1/quotes", stub.path)
	}
	if len(got) != 2 || got[0].Symbol != "AAPL" || got[0].Price != 190.5 || got[0].ChangePct != 1.25 || got[1].ChangePct != -0.5 {
		t.Fatalf("got %+v", got)
	}
}

func TestQuotes_EmptyResponse(t *testing.T) {
	got, err := New(&stubClient{body: `{"quotes":[]}`}).Quotes(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want no quotes", got, err)
	}
}

func TestQuotes_PropagatesError(t *testing.T) {
	_, err := New(&stubClient{err: platform.ErrUnavailable}).Quotes(context.Background())
	if !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
