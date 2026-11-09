package protocol

// ProjectHarness is a portable project configuration. Execution targets use
// durable node and directory IDs, so reordering a project cannot retarget scripts.
type ProjectHarness struct {
	Format       int               `json:"format"`
	Instructions string            `json:"instructions"`
	Skills       []ProjectSkill    `json:"skills"`
	Scripts      []ProjectScript   `json:"scripts"`
	Workflows    []ProjectWorkflow `json:"workflows"`
	Memory       *ProjectMemory    `json:"memory,omitempty"`
}

type ProjectMemory struct {
	Enabled bool   `json:"enabled"`
	Content string `json:"content"`
}

type ProjectSkill struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

type ProjectScript struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DeviceID    string   `json:"deviceId"`
	ProjectID   string   `json:"projectId"`
	Command     []string `json:"command"`
	Cwd         string   `json:"cwd"`
	TimeoutMS   int64    `json:"timeoutMs"`
}

type ProjectWorkflow struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Steps        []string `json:"steps"`
}

type ProjectHarnessRevision struct {
	ProjectID string `json:"projectId"`
	Revision  int64  `json:"revision"`
	Version   string `json:"version"`
	CreatedAt string `json:"createdAt"`
}

type ProjectHarnessState struct {
	ProjectID       string         `json:"projectId"`
	ProjectRevision int64          `json:"projectRevision"`
	Revision        int64          `json:"revision"`
	Version         string         `json:"version"`
	CreatedAt       string         `json:"createdAt"`
	Configuration   ProjectHarness `json:"configuration"`
}

type ProjectHarnessUpdate struct {
	ExpectedRevision        int64          `json:"expectedRevision"`
	ExpectedProjectRevision int64          `json:"expectedProjectRevision"`
	Configuration           ProjectHarness `json:"configuration"`
}

type ProjectHarnessHistory struct {
	Items      []ProjectHarnessRevision `json:"items"`
	NextBefore int64                    `json:"nextBefore,omitempty"`
}

// The effective context is immutable once a turn is admitted. It includes the
// project directory order and the exact project instructions sent to the model.
type ProjectTurnContext struct {
	SessionID    string              `json:"sessionId"`
	CommandID    string              `json:"commandId"`
	Project      Project             `json:"project"`
	Harness      ProjectHarnessState `json:"harness"`
	Instructions string              `json:"instructions"`
	CreatedAt    string              `json:"createdAt"`
}
