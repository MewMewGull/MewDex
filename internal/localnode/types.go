// Package localnode manages a local device service independently of any GUI.
package localnode

import (
	"mewdex/internal/hubproxy"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
	"mewdex/internal/webui"
)

type StartupInfo startup.Info

type StartupRequest struct {
	Enabled bool `json:"enabled"`
}

type WebWorkbenchSettings = webui.WebWorkbenchSettings

type Setup struct {
	Account       *protocol.MewDexAccountLogin `json:"account,omitempty"`
	Name          string                       `json:"name"`
	Hub           bool                         `json:"hub"`
	Executor      bool                         `json:"executor"`
	Gateway       bool                         `json:"gateway"`
	Listen        string                       `json:"listen,omitempty"`
	HubURL        string                       `json:"hubUrl,omitempty"`
	Invitation    string                       `json:"invitation,omitempty"`
	UseHubGateway bool                         `json:"useHubGateway,omitempty"`
	Projects      []protocol.Project           `json:"projects"`
}

type Request struct {
	Computer           *protocol.ComputerApproval `json:"computer,omitempty"`
	ModelRouting       *ModelRoutingSettings      `json:"modelRouting,omitempty"`
	WebWorkbench       *WebWorkbenchSettings      `json:"webWorkbench,omitempty"`
	ProjectID          string                     `json:"projectId,omitempty"`
	Operation          string                     `json:"operation"`
	Setup              *Setup                     `json:"setup,omitempty"`
	Projects           []protocol.Project         `json:"projects,omitempty"`
	ExpectedDeviceID   string                     `json:"expectedDeviceId,omitempty"`
	ExpectedInstanceID string                     `json:"expectedInstanceId,omitempty"`
	Backup             *BackupRequest             `json:"backup,omitempty"`
	Restore            *RestoreRequest            `json:"restore,omitempty"`
	Pairing            *PairingRequest            `json:"pairing,omitempty"`
	Job                *JobRequest                `json:"job,omitempty"`
	Startup            *StartupRequest            `json:"startup,omitempty"`
	Listener           *ListenerRequest           `json:"listener,omitempty"`
	HubProxy           *HubProxySettings          `json:"hubProxy,omitempty"`
	Upgrade            *UpgradeRequest            `json:"upgrade,omitempty"`
	Cleanup            *CleanupRequest            `json:"cleanup,omitempty"`
}

type CleanupRequest struct {
	ID string `json:"id,omitempty"`
}
type CleanupInfo struct {
	ID                    string `json:"id,omitempty"`
	DeviceID              string `json:"deviceId"`
	Name                  string `json:"name"`
	CreatedAt             string `json:"createdAt,omitempty"`
	State                 string `json:"state"`
	Pending               bool   `json:"pending"`
	UnconfirmedExecutions int    `json:"unconfirmedExecutions,omitempty"`
	CanPrepare            bool   `json:"canPrepare"`
	CanApply              bool   `json:"canApply"`
	NeedsRestart          bool   `json:"needsRestart"`
	Applied               bool   `json:"applied"`
	Message               string `json:"message,omitempty"`
}

type JobRequest struct {
	ID               string          `json:"id,omitempty"`
	Operation        string          `json:"operation,omitempty"`
	ExpectedDeviceID string          `json:"expectedDeviceId,omitempty"`
	Backup           *BackupRequest  `json:"backup,omitempty"`
	Restore          *RestoreRequest `json:"restore,omitempty"`
	Upgrade          *UpgradeRequest `json:"upgrade,omitempty"`
}

type OperationProgress struct {
	Phase      string `json:"phase"`
	Files      int    `json:"files"`
	TotalFiles int    `json:"totalFiles"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"totalBytes"`
}

type JobInfo struct {
	ID              string            `json:"id"`
	Operation       string            `json:"operation"`
	State           string            `json:"state"`
	CreatedAt       string            `json:"createdAt"`
	UpdatedAt       string            `json:"updatedAt"`
	Progress        OperationProgress `json:"progress"`
	CancelRequested bool              `json:"cancelRequested"`
	Backup          *BackupInfo       `json:"backup,omitempty"`
	Restore         *RestoreInfo      `json:"restore,omitempty"`
	Upgrade         *UpgradeInfo      `json:"upgrade,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type PairingRequest struct {
	Invitation string `json:"invitation"`
}

type RestoreRequest struct {
	Path       string `json:"path,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	ID         string `json:"id,omitempty"`
}

type UpgradeRequest struct {
	ID string `json:"id"`
}
type UpgradeInfo struct {
	ID                  string             `json:"id"`
	DeviceID            string             `json:"deviceId"`
	Name                string             `json:"name"`
	CreatedAt           string             `json:"createdAt"`
	Hub                 bool               `json:"hub"`
	Executor            bool               `json:"executor"`
	Gateway             bool               `json:"gateway"`
	Projects            []protocol.Project `json:"projects"`
	SourceEngineVersion string             `json:"sourceEngineVersion,omitempty"`
	TargetEngineVersion string             `json:"targetEngineVersion"`
	Applied             bool               `json:"applied"`
}

type RestoreInfo struct {
	ID                     string             `json:"id"`
	DeviceID               string             `json:"deviceId"`
	Name                   string             `json:"name"`
	CreatedAt              string             `json:"createdAt"`
	Hub                    bool               `json:"hub"`
	Executor               bool               `json:"executor"`
	Gateway                bool               `json:"gateway"`
	Projects               []protocol.Project `json:"projects"`
	RequiresPairing        bool               `json:"requiresPairing"`
	RequiresModelLogin     bool               `json:"requiresModelLogin"`
	PreviousStatePreserved bool               `json:"previousStatePreserved"`
	Applied                bool               `json:"applied"`
}

type BackupRequest struct {
	Path       string `json:"path"`
	Passphrase string `json:"passphrase"`
}

type BackupInfo struct {
	Path                  string `json:"path"`
	DeviceID              string `json:"deviceId"`
	CreatedAt             string `json:"createdAt"`
	Files                 int    `json:"files"`
	Bytes                 int64  `json:"bytes"`
	EngineHistoryIncluded bool   `json:"engineHistoryIncluded"`
}

type Snapshot struct {
	ModelRouting         *ModelRoutingSettings `json:"modelRouting,omitempty"`
	StartupPreference    *bool                 `json:"startupPreference,omitempty"`
	Operations           []string              `json:"operations,omitempty"`
	Configured           bool                  `json:"configured"`
	SetupPending         bool                  `json:"setupPending"`
	State                string                `json:"state"`
	Ready                bool                  `json:"ready"`
	DeviceID             string                `json:"deviceId,omitempty"`
	InstanceID           string                `json:"instanceId,omitempty"`
	Name                 string                `json:"name,omitempty"`
	Hub                  bool                  `json:"hub"`
	Executor             bool                  `json:"executor"`
	Gateway              bool                  `json:"gateway"`
	BaseURL              string                `json:"baseUrl,omitempty"`
	HubURL               string                `json:"hubUrl,omitempty"`
	Listen               string                `json:"listen,omitempty"`
	UseHubGateway        bool                  `json:"useHubGateway"`
	Projects             []protocol.Project    `json:"projects"`
	EnginePresent        bool                  `json:"enginePresent"`
	ExistingModelAccount bool                  `json:"existingModelAccount"`
	PairingRequired      bool                  `json:"pairingRequired"`
	Message              string                `json:"message,omitempty"`

	HubConnection *protocol.NodeConnection `json:"hubConnection,omitempty"`
}

type Credentials struct {
	Token           string            `json:"token"`
	Identity        protocol.Identity `json:"identity"`
	ProtocolVersion int               `json:"protocolVersion"`
	HubDeviceID     string            `json:"hubDeviceId,omitempty"`
	InputProxy      *protocol.Device  `json:"inputProxy,omitempty"`
}

type Connection struct {
	BaseURL     string      `json:"baseURL"`
	Credentials Credentials `json:"credentials"`
}

type Result struct {
	Computer     *protocol.ComputerLocalStatus `json:"computer,omitempty"`
	WebWorkbench *WebWorkbenchSettings         `json:"webWorkbench,omitempty"`
	Project      *protocol.Project             `json:"project,omitempty"`
	Snapshot     Snapshot                      `json:"snapshot"`
	Connection   *Connection                   `json:"connection,omitempty"`
	Backup       *BackupInfo                   `json:"backup,omitempty"`
	Restore      *RestoreInfo                  `json:"restore,omitempty"`
	Upgrade      *UpgradeInfo                  `json:"upgrade,omitempty"`
	Job          *JobInfo                      `json:"job,omitempty"`
	Startup      *StartupInfo                  `json:"startup,omitempty"`
	HubProxy     *HubProxyInfo                 `json:"hubProxy,omitempty"`
	SSHConfig    *hubproxy.SSHConfigInfo       `json:"sshConfig,omitempty"`
	SSHTest      *hubproxy.ConnectionTest      `json:"sshTest,omitempty"`
	ExistingHub  *HubDiscovery                 `json:"existingHub,omitempty"`
	Cleanup      *CleanupInfo                  `json:"cleanup,omitempty"`
}

type Response struct {
	Version int     `json:"version"`
	OK      bool    `json:"ok"`
	Result  *Result `json:"result,omitempty"`
	Error   string  `json:"error,omitempty"`
}
