package protocol

type ExtensionRequest struct {
	Section         string `json:"section"`
	Action          string `json:"action,omitempty"`
	ExpectedVersion string `json:"expectedVersion,omitempty"`
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	Instructions    string `json:"instructions,omitempty"`
	Path            string `json:"path,omitempty"`
	Key             string `json:"key,omitempty"`
	Hash            string `json:"hash,omitempty"`
	Enabled         bool   `json:"enabled"`
	Event           string `json:"event,omitempty"`
	Command         string `json:"command,omitempty"`
	Matcher         string `json:"matcher,omitempty"`
	TimeoutSec      int64  `json:"timeoutSec,omitempty"`
	Source          string `json:"source,omitempty"`
	PluginID        string `json:"pluginId,omitempty"`
	Marketplace     string `json:"marketplace,omitempty"`
	MarketplacePath string `json:"marketplacePath,omitempty"`
	GenerateMemory  bool   `json:"generateMemory"`
	UseMemory       bool   `json:"useMemory"`
}

type ManagedSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
}

type RuntimeHook struct {
	Key         string `json:"key"`
	EventName   string `json:"eventName"`
	HandlerType string `json:"handlerType"`
	Command     string `json:"command,omitempty"`
	Matcher     string `json:"matcher,omitempty"`
	Enabled     bool   `json:"enabled"`
	TrustStatus string `json:"trustStatus"`
	CurrentHash string `json:"currentHash"`
	Source      string `json:"source"`
	SourcePath  string `json:"sourcePath"`
	IsManaged   bool   `json:"isManaged"`
	TimeoutSec  int64  `json:"timeoutSec"`
}

type RuntimePlugin struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Version         string `json:"version,omitempty"`
	Marketplace     string `json:"marketplace"`
	MarketplacePath string `json:"marketplacePath,omitempty"`
	Source          string `json:"source"`
	Installed       bool   `json:"installed"`
	Enabled         bool   `json:"enabled"`
	InstallPolicy   string `json:"installPolicy"`
	Availability    string `json:"availability"`
	Fingerprint     string `json:"fingerprint"`
}

type ExtensionCatalog struct {
	Section        string          `json:"section"`
	ConfigVersion  string          `json:"configVersion"`
	Skills         []ManagedSkill  `json:"skills"`
	Hooks          []RuntimeHook   `json:"hooks"`
	Plugins        []RuntimePlugin `json:"plugins"`
	Warnings       []string        `json:"warnings"`
	HooksEnabled   bool            `json:"hooksEnabled"`
	PluginsEnabled bool            `json:"pluginsEnabled"`
	MemoryEnabled  bool            `json:"memoryEnabled"`
	GenerateMemory bool            `json:"generateMemory"`
	UseMemory      bool            `json:"useMemory"`
}

type ExtensionMutation struct {
	Status          string   `json:"status"`
	AppsNeedingAuth []string `json:"appsNeedingAuth,omitempty"`
}
