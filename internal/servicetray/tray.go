// Package servicetray presents the operating service's native controls.
package servicetray

import "context"

type Role string

const (
	InputRole    Role = "input"
	ExecutorRole Role = "executor"
	HubRole      Role = "hub"
)

type Item struct {
	ID        string `json:"id,omitempty"`
	Label     string `json:"label,omitempty"`
	Question  string `json:"question,omitempty"`
	Checked   bool   `json:"checked,omitempty"`
	Separator bool   `json:"separator,omitempty"`
}

type Options struct {
	Role   Role
	Title  string
	Menu   func() []Item
	Action func(context.Context, string) (string, error)
}

type view struct {
	Role    Role   `json:"role,omitempty"`
	Title   string `json:"title"`
	Items   []Item `json:"items"`
	Message string `json:"message,omitempty"`
	Error   bool   `json:"error,omitempty"`
}

type event struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Visible bool   `json:"visible,omitempty"`
	Message string `json:"message,omitempty"`
}
