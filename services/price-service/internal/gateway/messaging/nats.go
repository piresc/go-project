// Package messaging holds the NATS adapter.
package messaging

import "github.com/piresc/gokit/nats"

// NATS publishes and consumes events. Placeholder: no subjects yet.
type NATS struct {
	client *nats.Client
}

// NewNATS builds a NATS adapter on the given client.
func NewNATS(client *nats.Client) *NATS {
	return &NATS{client: client}
}
