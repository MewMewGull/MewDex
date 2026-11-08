package protocol

// Computer sessions require an explicit, expiring local window grant. The Hub
// selects a paired execution node and preserves the original owner on retries.
type ComputerRequest struct {
	OwnerID       string          `json:"ownerId"`
	TurnID        string          `json:"turnId,omitempty"`
	SessionID     string          `json:"sessionId,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	ObservationID string          `json:"observationId,omitempty"`
	Action        *ComputerAction `json:"action,omitempty"`
}

type ComputerAction struct {
	Kind   string   `json:"kind"`
	X      *int     `json:"x,omitempty"`
	Y      *int     `json:"y,omitempty"`
	Button string   `json:"button,omitempty"`
	Text   string   `json:"text,omitempty"`
	Keys   []string `json:"keys,omitempty"`
	Scroll int      `json:"scroll,omitempty"`
}

type ComputerWindow struct {
	ID        string `json:"id"`
	ProcessID uint32 `json:"processId"`
	Title     string `json:"title"`
}

type ComputerSession struct {
	ID                string          `json:"id"`
	DeviceID          string          `json:"deviceId"`
	ProjectID         string          `json:"projectId"`
	OwnerID           string          `json:"ownerId"`
	InstanceID        string          `json:"instanceId"`
	Reason            string          `json:"reason"`
	State             string          `json:"state"`
	Revision          int64           `json:"revision"`
	Window            *ComputerWindow `json:"window,omitempty"`
	ExpiresAt         string          `json:"expiresAt"`
	LastActionID      string          `json:"lastActionId,omitempty"`
	LastActionState   string          `json:"lastActionState,omitempty"`
	LastObservationID string          `json:"lastObservationId,omitempty"`
	Error             string          `json:"error,omitempty"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
}

type ComputerObservation struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId"`
	Image        string `json:"image"`
	MimeType     string `json:"mimeType"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	WindowWidth  int    `json:"windowWidth"`
	WindowHeight int    `json:"windowHeight"`
	CreatedAt    string `json:"createdAt"`
}

type ComputerStatus struct {
	Supported bool             `json:"supported"`
	Reason    string           `json:"reason,omitempty"`
	Session   *ComputerSession `json:"session,omitempty"`
}

type ComputerResult struct {
	ExecutionTarget *ExecutionTarget     `json:"executionTarget,omitempty"`
	Status          ComputerStatus       `json:"status"`
	Observation     *ComputerObservation `json:"observation,omitempty"`
}

type ComputerApproval struct {
	SessionID        string `json:"sessionId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	WindowID         string `json:"windowId"`
	DurationSeconds  int    `json:"durationSeconds"`
}

type ComputerLocalStatus struct {
	Status  ComputerStatus   `json:"status"`
	Windows []ComputerWindow `json:"windows"`
}

type ComputerStopRequest struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
}
