package engine

import (
	"context"
	"encoding/json"
)

type Notification struct {
	Method    string
	Params    json.RawMessage
	RequestID json.RawMessage
}

// Engine keeps upstream JSON-RPC inside the executor boundary.
type Engine interface {
	Call(context.Context, string, any, any) error
	Respond(context.Context, json.RawMessage, json.RawMessage) error
	Reject(context.Context, json.RawMessage, string) error
	Notifications() <-chan Notification
	ExecutionEnvironment() (ExecutionEnvironment, error)
	Close() error
}
