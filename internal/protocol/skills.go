package protocol

// SkillOption is a discoverable, enabled skill owned by the model runtime.
type SkillOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
}

type SkillCatalog struct {
	Skills []SkillOption `json:"skills"`
	Errors []string      `json:"errors"`
}
