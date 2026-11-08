package protocol

// ProjectDirectory identifies a durable directory binding on an execution node.
// Paths use the owning node's native syntax and are never resolved on the Hub.
type ProjectDirectory struct {
	DeviceID  string `json:"deviceId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
}

type HubProjectRequest struct {
	Name             string             `json:"name"`
	Category         string             `json:"category,omitempty"`
	Directories      []ProjectDirectory `json:"directories"`
	ExpectedRevision int64              `json:"expectedRevision"`
}

// Terminal requests are issued by the Hub over a paired control connection.
// Input adapters cannot submit these operations directly.
type TerminalRequest struct {
	WorkspaceIDs    []string `json:"workspaceIds,omitempty"`
	OwnerID         string   `json:"ownerId"`
	JobID           string   `json:"jobId,omitempty"`
	Command         []string `json:"command,omitempty"`
	Cwd             string   `json:"cwd,omitempty"`
	AccessMode      string   `json:"accessMode,omitempty"`
	TimeoutMS       int64    `json:"timeoutMs,omitempty"`
	YieldMS         int64    `json:"yieldMs,omitempty"`
	HarnessVersion  string   `json:"harnessVersion,omitempty"`
	HarnessRevision int64    `json:"harnessRevision,omitempty"`
}

// ExecutionTarget captures the machine and workspace at dispatch time so
// historical output remains attributable after a device or project is renamed.
type ExecutionTarget struct {
	DeviceID        string `json:"deviceId"`
	DeviceName      string `json:"deviceName,omitempty"`
	Platform        string `json:"platform,omitempty"`
	ProjectID       string `json:"projectId"`
	ProjectName     string `json:"projectName,omitempty"`
	Workspace       string `json:"workspace,omitempty"`
	AccessMode      string `json:"accessMode,omitempty"`
	HarnessVersion  string `json:"harnessVersion,omitempty"`
	HarnessRevision int64  `json:"harnessRevision,omitempty"`
}

type TerminalJob struct {
	WorkspaceIDs []string `json:"workspaceIds,omitempty"`
	ID           string   `json:"id"`
	OwnerID      string   `json:"ownerId"`
	ProjectID    string   `json:"projectId"`
	State        string   `json:"state"`
	Cwd          string   `json:"cwd"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"`
	ExitCode     *int     `json:"exitCode,omitempty"`
	Error        string   `json:"error,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}
