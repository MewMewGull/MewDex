// Package protocol defines the transport-neutral MewDex v1 contract.
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

const Version = 1

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func JSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

type Project struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Category    string             `json:"category,omitempty"`
	Directories []ProjectDirectory `json:"directories,omitempty"`
	Revision    int64              `json:"revision,omitempty"`
}

type WorkspaceRequest struct {
	Path         string `json:"path"`
	Name         string `json:"name,omitempty"`
	Category     string `json:"category,omitempty"`
	CreateFolder bool   `json:"createFolder,omitempty"`
}
type WorkspaceDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type WorkspaceListing struct {
	Path        string               `json:"path"`
	Parent      string               `json:"parent"`
	Directories []WorkspaceDirectory `json:"directories"`
	Projects    []Project            `json:"projects"`
}

type Device struct {
	ID              string    `json:"id"`
	HostID          string    `json:"hostId,omitempty"`
	Name            string    `json:"name"`
	Platform        string    `json:"platform"`
	Roles           []string  `json:"roles"`
	Projects        []Project `json:"projects"`
	Online          bool      `json:"online"`
	LastSeen        string    `json:"lastSeen"`
	ProtocolVersion int       `json:"protocolVersion"`
	Capabilities    []string  `json:"capabilities,omitempty"`
}

type Policy struct {
	Mode              string  `json:"mode"`
	NoTests           bool    `json:"noTests"`
	Instructions      string  `json:"instructions"`
	BaseInstructions  *string `json:"baseInstructions,omitempty"`
	Completion        string  `json:"completion"`
	Model             string  `json:"model,omitempty"`
	ReasoningEffort   string  `json:"reasoningEffort,omitempty"`
	CollaborationMode string  `json:"collaborationMode,omitempty"`
	AccessMode        string  `json:"accessMode,omitempty"`
}

type Session struct {
	ID                 string             `json:"id"`
	DeviceID           string             `json:"deviceId"`
	ProjectID          string             `json:"projectId"`
	Title              string             `json:"title"`
	Archived           bool               `json:"archived,omitempty"`
	Pinned             bool               `json:"pinned,omitempty"`
	EngineID           string             `json:"engineId"`
	EngineStarted      bool               `json:"engineStarted"`
	ExecutionScopeID   string             `json:"executionScopeId,omitempty"`
	ScopeAccessMode    string             `json:"scopeAccessMode,omitempty"`
	ContextUsage       *ContextUsage      `json:"contextUsage,omitempty"`
	PolicyVersion      string             `json:"policyVersion"`
	PolicyInstructions string             `json:"policyInstructions"`
	BaseVersion        string             `json:"baseVersion"`
	Model              string             `json:"model"`
	DefaultModel       string             `json:"defaultModel"`
	TurnID             string             `json:"turnId"`
	LastStartID        string             `json:"lastStartId,omitempty"`
	State              string             `json:"state"`
	StopStatus         string             `json:"stopStatus"`
	Frozen             bool               `json:"frozen"`
	ExecutionRecovery  *ExecutionRecovery `json:"executionRecovery,omitempty"`
	Policy             Policy             `json:"policy"`
	CreatedAt          string             `json:"createdAt"`
	UpdatedAt          string             `json:"updatedAt"`
}

// SessionPatch edits shared presentation metadata without changing execution.
type SessionPatch struct {
	Title    *string `json:"title,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
}

// SessionActivity is an input-facing overview; request bodies and queued input
// remain available only in their conversation-specific endpoints.
type SessionActivity struct {
	Session              Session `json:"session"`
	PendingInteractions  int     `json:"pendingInteractions"`
	BlockingInteractions int     `json:"blockingInteractions"`
	QueuedInputs         int     `json:"queuedInputs"`
	UnconfirmedInputs    int     `json:"unconfirmedInputs"`
	FailedInputs         int     `json:"failedInputs"`
	NeedsAttention       bool    `json:"needsAttention"`
}

// ExecutionRecovery advertises an executor-owned decision. Input clients only
// submit the opaque ID; the OS proof and captured attempt stay private.
type ExecutionRecovery struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// ExecutionUnconfirmed is shared by startup, restored copies and local recovery.
// It describes an unresolved attempt, not permission to release its project.
func (s Session) ExecutionUnconfirmed() bool {
	return s.State == "unknown" || s.State == "running" || s.State == "stopping" || s.StopStatus == "background-running" || s.StopStatus == "processes-unchecked" || s.StopStatus == "unconfirmed"
}

type Input struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	AttachmentID string `json:"attachmentId,omitempty"`
}

type Command struct {
	Terminal              *TerminalRequest  `json:"terminal,omitempty"`
	Workspace             *WorkspaceRequest `json:"workspace,omitempty"`
	Models                *ModelListRequest `json:"models,omitempty"`
	File                  *FileRequest      `json:"file,omitempty"`
	ID                    string            `json:"id"`
	DeviceID              string            `json:"deviceId"`
	SessionID             string            `json:"sessionId,omitempty"`
	ProjectID             string            `json:"projectId,omitempty"`
	Kind                  string            `json:"kind"`
	Title                 string            `json:"title,omitempty"`
	ExpectedTurnID        string            `json:"expectedTurnId,omitempty"`
	ExpectedPolicyVersion string            `json:"expectedPolicyVersion,omitempty"`
	Input                 []Input           `json:"input,omitempty"`
	Policy                *Policy           `json:"policy,omitempty"`
	RequestID             string            `json:"requestId,omitempty"`
	ExpectedRevision      int64             `json:"expectedRevision,omitempty"`
	Response              json.RawMessage   `json:"response,omitempty"`
	SourceID              string            `json:"sourceId"`
	CreatedAt             string            `json:"createdAt"`
}

type CommandResult struct {
	CommandID string          `json:"commandId"`
	State     string          `json:"state"`
	Session   *Session        `json:"session,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// QueueItem is an immutable input and policy snapshot with executor-owned progress.
// Position is allocated by the executor, independent of input clocks and transports.
type QueueItem struct {
	ID                 string  `json:"id"`
	DeviceID           string  `json:"deviceId"`
	SessionID          string  `json:"sessionId"`
	ProjectID          string  `json:"projectId"`
	SourceID           string  `json:"sourceId"`
	Position           int64   `json:"position"`
	Revision           int64   `json:"revision"`
	State              string  `json:"state"`
	Input              []Input `json:"input"`
	Policy             Policy  `json:"policy"`
	PolicyVersion      string  `json:"policyVersion"`
	PolicyInstructions string  `json:"policyInstructions"`
	TurnID             string  `json:"turnId,omitempty"`
	Error              string  `json:"error,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`
}

type ExecutionObservation struct {
	EngineState string `json:"engineState"`
	TurnID      string `json:"turnId,omitempty"`
	TurnStatus  string `json:"turnStatus,omitempty"`
	ClientID    string `json:"clientId,omitempty"`
	Matched     bool   `json:"matched"`
}

type Event struct {
	Sequence  int64           `json:"sequence"`
	ID        string          `json:"id"`
	DeviceID  string          `json:"deviceId"`
	SessionID string          `json:"sessionId,omitempty"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt"`
}

type EventPage struct {
	Events   []Event `json:"events"`
	Before   int64   `json:"before"`
	After    int64   `json:"after"`
	HasMore  bool    `json:"hasMore"`
	HasNewer bool    `json:"hasNewer"`
}

// CommandReference omits input, policies and interaction replies. It is enough
// to find an existing receipt after an input application restarts.
type CommandReference struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId"`
	SessionID string `json:"sessionId,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
}

type PendingCommandPage struct {
	Commands []CommandReference `json:"commands"`
	After    int64              `json:"after"`
	HasMore  bool               `json:"hasMore"`
}

type NetworkSnapshot struct {
	Capabilities        []string             `json:"capabilities,omitempty"`
	Cursor              int64                `json:"cursor"`
	Identity            Identity             `json:"identity"`
	Devices             []Device             `json:"devices"`
	Sessions            []Session            `json:"sessions"`
	ConversationUpdates []ConversationUpdate `json:"conversationUpdates"`
}

type Identity struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	DeviceID  string `json:"deviceId,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	CreatedAt string `json:"createdAt"`
	Access    Access `json:"access"`
	AccountID string `json:"accountId,omitempty"`
}

// MewDexAccount authenticates a user of the Hub. ModelAccount belongs to its coordinator.
type MewDexAccount struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

type MewDexAccountLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ServerAuthentication struct {
	Methods           []string `json:"methods"`
	DefaultMethod     string   `json:"defaultMethod"`
	AccountConfigured bool     `json:"accountConfigured"`
}

type ServerInfo struct {
	HubDeviceID     string               `json:"hubDeviceId,omitempty"`
	Name            string               `json:"name"`
	ProtocolVersion int                  `json:"protocolVersion"`
	Capabilities    []string             `json:"capabilities"`
	Authentication  ServerAuthentication `json:"authentication"`
	InputProxy      *Device              `json:"inputProxy,omitempty"`
}

// Access is a network grant, independent of the execution sandbox and model account.
type Access struct {
	AllDevices bool           `json:"allDevices"`
	Targets    []TargetAccess `json:"targets"`
	Gateway    bool           `json:"gateway"`
}

type TargetAccess struct {
	DeviceID   string   `json:"deviceId"`
	ProjectIDs []string `json:"projectIds"`
}

func (a Access) Allows(device, project string) bool {
	if a.AllDevices {
		return true
	}
	for _, target := range a.Targets {
		if target.DeviceID != device {
			continue
		}
		if project == "" || len(target.ProjectIDs) == 0 {
			return true
		}
		for _, id := range target.ProjectIDs {
			if id == project {
				return true
			}
		}
	}
	return false
}

func DefaultAccess(role string) Access {
	return Access{AllDevices: role == "admin" || role == "client", Targets: []TargetAccess{}}
}

func (a Access) Normalized() Access {
	if a.Targets == nil {
		a.Targets = []TargetAccess{}
	}
	for i := range a.Targets {
		if a.Targets[i].ProjectIDs == nil {
			a.Targets[i].ProjectIDs = []string{}
		}
	}
	return a
}

type Frame struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Attachment struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MediaType       string `json:"mediaType"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
	CreatedAt       string `json:"createdAt"`
	OwnerID         string `json:"ownerId,omitempty"`
	TargetDeviceID  string `json:"targetDeviceId,omitempty"`
	TargetProjectID string `json:"targetProjectId,omitempty"`
}

type ModelAccount struct {
	SignedIn        bool   `json:"signedIn"`
	Type            string `json:"type"`
	PlanType        string `json:"planType,omitempty"`
	LoginSupported  bool   `json:"loginSupported"`
	Email           string `json:"email,omitempty"`
	LogoutSupported bool   `json:"logoutSupported"`
}
type RateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}
type RateLimitSnapshot struct {
	LimitID   string           `json:"limitId,omitempty"`
	LimitName string           `json:"limitName,omitempty"`
	Primary   *RateLimitWindow `json:"primary"`
	Secondary *RateLimitWindow `json:"secondary"`
}
type ModelRateLimits struct {
	RateLimits          *RateLimitSnapshot           `json:"rateLimits"`
	RateLimitsByLimitID map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
}
type ContextUsage struct {
	InputTokens        int64  `json:"inputTokens"`
	CachedInputTokens  int64  `json:"cachedInputTokens"`
	OutputTokens       int64  `json:"outputTokens"`
	TotalTokens        int64  `json:"totalTokens"`
	ModelContextWindow *int64 `json:"modelContextWindow"`
}
type DeviceLogin struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
}

// Interaction is an executor-owned request that any authorized input can answer.
// Engine RPC identifiers and response formats stay inside the executor.
type Interaction struct {
	ExecutionScopeID string           `json:"executionScopeId,omitempty"`
	ID               string           `json:"id"`
	SessionID        string           `json:"sessionId"`
	TurnID           string           `json:"turnId,omitempty"`
	Kind             string           `json:"kind"`
	State            string           `json:"state"`
	Blocking         bool             `json:"blocking"`
	CanAccept        bool             `json:"canAccept"`
	Title            string           `json:"title"`
	Message          string           `json:"message"`
	Command          string           `json:"command,omitempty"`
	Cwd              string           `json:"cwd,omitempty"`
	Details          string           `json:"details,omitempty"`
	Permissions      *PermissionGrant `json:"permissions,omitempty"`
	FilePatch        string           `json:"filePatch,omitempty"`
	URL              string           `json:"url,omitempty"`
	Questions        []InputQuestion  `json:"questions"`
	CreatedAt        string           `json:"createdAt"`
}

// PermissionGrant describes additional access for the current turn. It is
// supplied by the executor, never by an input client's reply.
type PermissionGrant struct {
	ReadPaths  []string `json:"readPaths"`
	WritePaths []string `json:"writePaths"`
	Network    bool     `json:"network"`
}

type InputOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type InputQuestion struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Format      string          `json:"format,omitempty"`
	Required    bool            `json:"required"`
	Options     []InputOption   `json:"options"`
	AllowText   bool            `json:"allowText"`
	Default     json.RawMessage `json:"default,omitempty"`
	Minimum     *float64        `json:"minimum,omitempty"`
	Maximum     *float64        `json:"maximum,omitempty"`
	MinLength   *int            `json:"minLength,omitempty"`
	MaxLength   *int            `json:"maxLength,omitempty"`
}

type InteractionReply struct {
	Action  string                     `json:"action"`
	Answers map[string][]string        `json:"answers,omitempty"`
	Values  map[string]json.RawMessage `json:"values,omitempty"`
}

type PolicySnapshot struct {
	ID                    string `json:"id"`
	SessionID             string `json:"sessionId"`
	Version               string `json:"version"`
	Policy                Policy `json:"policy"`
	DeveloperInstructions string `json:"developerInstructions"`
	CreatedAt             string `json:"createdAt"`
}

type PromptSource struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Path     string `json:"path,omitempty"`
	Text     string `json:"text"`
	Note     string `json:"note,omitempty"`
	ReadOnly bool   `json:"readOnly"`
	Enabled  bool   `json:"enabled"`
}

type PromptInspection struct {
	ProjectID             string         `json:"projectId"`
	Version               string         `json:"version"`
	Policy                Policy         `json:"policy"`
	DeveloperInstructions string         `json:"developerInstructions"`
	Sources               []PromptSource `json:"sources"`
	Notes                 []string       `json:"notes"`
}
