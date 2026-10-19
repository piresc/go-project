package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/piresc/gokit/httpclient"
	"github.com/piresc/gokit/tracing"
	"github.com/sony/gobreaker/v2"
)

const (
	defaultHTTPTimeout      = 5 * time.Second
	defaultFailureThreshold = 5
	defaultOpenTimeout      = 30 * time.Second
	maxResponseBytes        = 10 << 20
)

// ErrUnavailable is returned (wrapped) while a client's circuit breaker is
// open, without calling the upstream.
var ErrUnavailable = errors.New("upstream unavailable")

// HTTPClientSettings configures one named upstream, under
// "service.http_clients.<name>".
type HTTPClientSettings struct {
	BaseURL string `yaml:"base_url" mapstructure:"base_url"`
	// Timeout is per attempt. Defaults to 5s.
	Timeout time.Duration `yaml:"timeout" mapstructure:"timeout"`
	// FailureThreshold is the number of consecutive failed calls that opens
	// the breaker. Defaults to 5.
	FailureThreshold uint32 `yaml:"failure_threshold" mapstructure:"failure_threshold"`
	// OpenTimeout is how long the breaker stays open before a trial call.
	// Defaults to 30s.
	OpenTimeout time.Duration `yaml:"open_timeout" mapstructure:"open_timeout"`
}

// HTTPClient is a gokit httpclient behind a circuit breaker. One GetJSON call
// is one breaker call, even though gokit makes up to 3 attempts inside it.
type HTTPClient struct {
	name    string
	client  *httpclient.Client
	breaker *gobreaker.CircuitBreaker[[]byte]
}

// statusError is a non-2xx upstream response.
type statusError struct{ code int }

func (e *statusError) Error() string {
	return fmt.Sprintf("HTTP error %d: %s", e.code, http.StatusText(e.code))
}

func newHTTPClient(name string, s HTTPClientSettings, tracer tracing.Tracer) *HTTPClient {
	if s.Timeout <= 0 {
		s.Timeout = defaultHTTPTimeout
	}
	if s.FailureThreshold == 0 {
		s.FailureThreshold = defaultFailureThreshold
	}
	if s.OpenTimeout <= 0 {
		s.OpenTimeout = defaultOpenTimeout
	}
	return &HTTPClient{
		name:   name,
		client: httpclient.NewClient(httpclient.Config{BaseURL: s.BaseURL, Timeout: s.Timeout, Tracer: tracer}),
		breaker: gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
			Name:    name,
			Timeout: s.OpenTimeout,
			// Interval 0: counts never reset while closed, so slow traffic
			// (a call a minute) can still trip the breaker.
			Interval: 0,
			ReadyToTrip: func(c gobreaker.Counts) bool {
				return c.ConsecutiveFailures >= s.FailureThreshold
			},
			IsSuccessful: isSuccessful,
		}),
	}
}

// isSuccessful is false for transport errors, timeouts and 5xx. A 4xx is the
// caller's fault, and a canceled caller says nothing about the upstream.
func isSuccessful(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return true
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.code < http.StatusInternalServerError
	}
	return false
}

// GetJSON GETs endpoint and decodes the JSON body into out. It understands the
// gokit envelope ({"status", "data", "error"} or {"success", ...}) as well as plain JSON. A nil out
// discards the body. While the breaker is open it returns ErrUnavailable.
func (c *HTTPClient) GetJSON(ctx context.Context, endpoint string, out any) error {
	body, err := c.breaker.Execute(func() ([]byte, error) {
		resp, err := c.client.Do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, &statusError{code: resp.StatusCode}
		}
		return io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	})
	switch {
	case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
		return fmt.Errorf("http client %s: %w", c.name, ErrUnavailable)
	case err != nil:
		return fmt.Errorf("http client %s: GET %s: %w", c.name, endpoint, err)
	}
	if out == nil {
		return nil
	}
	return decodeJSONBody(body, out)
}

// decodeJSONBody unwraps the gokit API envelope when present: the
// {"status","data","error"} form written by response.Success and
// response.Error, or a {"success","data","error"} form. Other JSON is decoded
// as is.
func decodeJSONBody(body []byte, out any) error {
	var env struct {
		Status  *string         `json:"status"`
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && (env.Status != nil || env.Success != nil) {
		ok := env.Success != nil && *env.Success
		if env.Status != nil {
			ok = *env.Status == "success"
		}
		if !ok {
			return fmt.Errorf("API error: %s", env.Error)
		}
		if len(env.Data) == 0 {
			return nil
		}
		body = env.Data
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}

// initHTTPClients builds every configured client, failing on an empty base URL.
func (a *App[S]) initHTTPClients() error {
	a.httpClients = make(map[string]*HTTPClient, len(a.Settings.HTTPClients))
	for name, s := range a.Settings.HTTPClients {
		if s.BaseURL == "" {
			return fmt.Errorf("http client %s: base_url is empty", name)
		}
		a.httpClients[name] = newHTTPClient(name, s, a.Tracer)
	}
	return nil
}

// HTTPClient returns the client configured under service.http_clients.<name>.
func (a *App[S]) HTTPClient(name string) (*HTTPClient, error) {
	c, ok := a.httpClients[name]
	if !ok {
		return nil, fmt.Errorf("http client %s not configured", name)
	}
	return c, nil
}
