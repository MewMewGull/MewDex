package protocol

type ModelListRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ReasoningOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}

// ModelOption is public picker metadata, not proof of model-account access.
type ModelOption struct {
	ID                        string            `json:"id"`
	Model                     string            `json:"model"`
	DisplayName               string            `json:"displayName"`
	Description               string            `json:"description"`
	IsDefault                 bool              `json:"isDefault"`
	DefaultReasoningEffort    string            `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []ReasoningOption `json:"supportedReasoningEfforts"`
	InputModalities           []string          `json:"inputModalities,omitempty"`
}

// ModelRuntime identifies the adapter that actually completed initialization.
// It does not assert account access to any catalog model.
type ModelRuntime struct {
	Adapter   string `json:"adapter"`
	Version   string `json:"version"`
	Transport string `json:"transport"`
}

type ModelPage struct {
	Models     []ModelOption `json:"models"`
	NextCursor string        `json:"nextCursor,omitempty"`
	Runtime    *ModelRuntime `json:"runtime,omitempty"`
}
