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
}

type ModelPage struct {
	Models     []ModelOption `json:"models"`
	NextCursor string        `json:"nextCursor,omitempty"`
}
