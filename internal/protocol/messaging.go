package protocol

// MessagingChannel is an administrator's view. Credentials never leave the Hub.
type MessagingChannel struct {
	Platform             string   `json:"platform"`
	Enabled              bool     `json:"enabled"`
	DeviceID             string   `json:"deviceId"`
	ProjectID            string   `json:"projectId"`
	AllowedUsers         []string `json:"allowedUsers"`
	AllowedChats         []string `json:"allowedChats"`
	AppID                string   `json:"appId"`
	BotUserID            string   `json:"botUserId"`
	AccountID            string   `json:"accountId"`
	CredentialConfigured bool     `json:"credentialConfigured"`
	State                string   `json:"state"`
	Detail               string   `json:"detail"`
	LastEventAt          string   `json:"lastEventAt"`
	LastSenderID         string   `json:"lastSenderId"`
	LastChatID           string   `json:"lastChatId"`
}

// A missing secret preserves the current credential; an empty secret clears it.
type MessagingUpdate struct {
	Enabled      bool     `json:"enabled"`
	DeviceID     string   `json:"deviceId"`
	ProjectID    string   `json:"projectId"`
	AllowedUsers []string `json:"allowedUsers"`
	AllowedChats []string `json:"allowedChats"`
	AppID        string   `json:"appId"`
	BotUserID    string   `json:"botUserId"`
	Secret       *string  `json:"secret,omitempty"`
}

type MessagingLogin struct {
	State string `json:"state"`
	URL   string `json:"url"`
}
