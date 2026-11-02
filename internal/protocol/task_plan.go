package protocol

type TaskPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// TaskPlan is a read-only projection of the engine's reported steps. Turn
// completion does not imply that every reported step was completed.
type TaskPlan struct {
	DeviceID    string         `json:"deviceId"`
	SessionID   string         `json:"sessionId"`
	TurnID      string         `json:"turnId"`
	Sequence    int64          `json:"sequence"`
	UpdatedAt   string         `json:"updatedAt"`
	Explanation string         `json:"explanation,omitempty"`
	Steps       []TaskPlanStep `json:"steps"`
	TurnStatus  string         `json:"turnStatus,omitempty"`
}
