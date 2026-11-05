package protocol

import "encoding/json"

type InputBridge struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type InputBridgeRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Owner  string          `json:"owner"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type InputBridgeResponse struct {
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}
