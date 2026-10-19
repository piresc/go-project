package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/piresc/gokit/response"
)

// countingServer answers every request with status and counts the requests.
func countingServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestHTTPClient_BreakerOpens(t *testing.T) {
	srv, hits := countingServer(t, http.StatusInternalServerError, "")
	c := newHTTPClient("market", HTTPClientSettings{BaseURL: srv.URL, FailureThreshold: 2}, nil)

	for i := 0; i < 2; i++ {
		err := c.GetJSON(context.Background(), "/v1/quotes", nil)
		if err == nil || errors.Is(err, ErrUnavailable) {
			t.Fatalf("call %d: want upstream error, got %v", i, err)
		}
	}
	if got := hits.Load(); got != 2*3 {
		t.Fatalf("server saw %d requests after 2 failed calls, want 6", got)
	}

	err := c.GetJSON(context.Background(), "/v1/quotes", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("third call: want ErrUnavailable, got %v", err)
	}
	if got := hits.Load(); got != 6 {
		t.Fatalf("breaker open but server saw %d requests, want 6", got)
	}
}

func TestHTTPClient_4xxDoesNotTrip(t *testing.T) {
	srv, hits := countingServer(t, http.StatusNotFound, "")
	c := newHTTPClient("market", HTTPClientSettings{BaseURL: srv.URL, FailureThreshold: 2}, nil)

	for i := 0; i < 5; i++ {
		err := c.GetJSON(context.Background(), "/v1/quotes", nil)
		if err == nil || errors.Is(err, ErrUnavailable) {
			t.Fatalf("call %d: want 404 error, got %v", i, err)
		}
	}
	if got := hits.Load(); got != 5 {
		t.Fatalf("server saw %d requests, want 5 (4xx is not retried)", got)
	}
}

func TestHTTPClient_DecodesJSON(t *testing.T) {
	srv, _ := countingServer(t, http.StatusOK, `{"quotes":[{"symbol":"A"}]}`)
	c := newHTTPClient("market", HTTPClientSettings{BaseURL: srv.URL}, nil)

	var out struct {
		Quotes []struct {
			Symbol string `json:"symbol"`
		} `json:"quotes"`
	}
	if err := c.GetJSON(context.Background(), "/v1/quotes", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Quotes) != 1 || out.Quotes[0].Symbol != "A" {
		t.Fatalf("got %+v", out)
	}
}

func TestHTTPClient_DecodesEnvelope(t *testing.T) {
	srv, _ := countingServer(t, http.StatusOK, `{"success":true,"data":{"n":7}}`)
	c := newHTTPClient("svc", HTTPClientSettings{BaseURL: srv.URL}, nil)

	var out struct {
		N int `json:"n"`
	}
	if err := c.GetJSON(context.Background(), "/x", &out); err != nil {
		t.Fatal(err)
	}
	if out.N != 7 {
		t.Fatalf("got %+v", out)
	}
}

func TestDecodeJSONBody_GokitResponse(t *testing.T) {
	ok := httptest.NewRecorder()
	response.Success(ok, http.StatusOK, map[string]int{"n": 7})
	var got struct{ N int }
	if err := decodeJSONBody(ok.Body.Bytes(), &got); err != nil || got.N != 7 {
		t.Fatalf("success: got %+v, err %v", got, err)
	}

	bad := httptest.NewRecorder()
	response.Error(bad, http.StatusBadRequest, "no such symbol")
	err := decodeJSONBody(bad.Body.Bytes(), &got)
	if err == nil || !strings.Contains(err.Error(), "no such symbol") {
		t.Fatalf("error envelope: got %v", err)
	}
}

func TestDecodeJSONBody_PlainJSON(t *testing.T) {
	// No envelope keys: decoded as is.
	var got struct{ A int }
	if err := decodeJSONBody([]byte(`{"a":3}`), &got); err != nil || got.A != 3 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestDecodeJSONBody_UnknownStatusIsPlain(t *testing.T) {
	// A status other than "success"/"error" is not an envelope.
	var got struct{ X int }
	if err := decodeJSONBody([]byte(`{"status":"OK","x":1}`), &got); err != nil || got.X != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestHTTPClient_EmptyBaseURL(t *testing.T) {
	writeConfig(t, "service:\n  http_clients:\n    market:\n      base_url: \"\"\n")
	_, err := New[struct{}]("svc")
	if err == nil || err.Error() != "http client market: base_url is empty" {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPClient_NotConfigured(t *testing.T) {
	writeConfig(t, "")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	_, err = a.HTTPClient("market")
	if err == nil || err.Error() != "http client market not configured" {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPClient_Configured(t *testing.T) {
	srv, _ := countingServer(t, http.StatusOK, `{}`)
	writeConfig(t, "service:\n  http_clients:\n    market:\n      base_url: "+srv.URL+"\n")
	a, err := New[struct{}]("svc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	c, err := a.HTTPClient("market")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.GetJSON(context.Background(), "/", nil); err != nil {
		t.Fatal(err)
	}
}
