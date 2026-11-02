package protocol

import "encoding/json"

// ConversationUpdate is a position in the Hub's durable reply history. It
// contains no message text and does not represent execution or task status.
type ConversationUpdate struct {
	DeviceID  string `json:"deviceId"`
	SessionID string `json:"sessionId"`
	Sequence  int64  `json:"sequence"`
}

func ConversationReply(event Event) bool {
	if event.Kind != "engine.item/completed" || event.SessionID == "" {
		return false
	}
	var payload struct {
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	return json.Unmarshal(event.Payload, &payload) == nil && payload.Item.Type == "agentMessage" && payload.Item.Text != ""
}
