package protocol

type FileRequest struct {
	Path  string `json:"path"`
	After string `json:"after,omitempty"`
}
type ProjectFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Directory  bool   `json:"directory"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}
type FileListing struct {
	Path    string        `json:"path"`
	Entries []ProjectFile `json:"entries"`
	Next    string        `json:"next,omitempty"`
	HasMore bool          `json:"hasMore"`
}

// An output is an explicitly requested immutable snapshot of a current file.
type OutputFile struct {
	ID         string     `json:"id"`
	CommandID  string     `json:"commandId"`
	SessionID  string     `json:"sessionId"`
	DeviceID   string     `json:"deviceId"`
	ProjectID  string     `json:"projectId"`
	Path       string     `json:"path"`
	Attachment Attachment `json:"attachment"`
	CreatedAt  string     `json:"createdAt"`
}

type FileExportRequest struct {
	Path       string     `json:"path"`
	Attachment Attachment `json:"attachment"`
}
