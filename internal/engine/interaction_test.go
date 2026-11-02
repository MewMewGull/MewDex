package engine

import (
	"encoding/json"
	"testing"

	"mewdex/internal/protocol"
)

func TestPermissionReplyCannotAddOrPersistPermissions(t *testing.T) {
	n := Notification{Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"turnId":"t","permissions":{"network":{"enabled":true}}}`)}
	view, err := RequestView(n, "s")
	if err != nil {
		t.Fatal(err)
	}
	if view.CanAccept {
		t.Fatal("unvalidated permission request offers an accept action")
	}
	// Only the executor can enable this after its local scope checks.
	view.CanAccept = true
	raw, err := ReplyFor(n, view, protocol.InteractionReply{Action: "accept", Values: map[string]json.RawMessage{"fileSystem": json.RawMessage(`{"write":["/"]}`)}})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Scope       string         `json:"scope"`
		Permissions map[string]any `json:"permissions"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Scope != "turn" || len(response.Permissions) != 1 || response.Permissions["fileSystem"] != nil {
		t.Fatalf("grant exceeded request: %s", raw)
	}
	if _, err = ReplyFor(n, view, protocol.InteractionReply{Action: "acceptForSession"}); err == nil {
		t.Fatal("unadvertised persistent permission accepted")
	}
}

func TestFormReplyValidatesRequiredFieldsAndOptions(t *testing.T) {
	n := Notification{Method: "mcpServer/elicitation/request", Params: json.RawMessage(`{"serverName":"demo","mode":"form","message":"choose","requestedSchema":{"type":"object","properties":{"project":{"type":"string","enum":["a","b"],"enumNames":["Alpha","Beta"]},"count":{"type":"integer","minimum":1,"maximum":3}},"required":["project","count"]}}`)}
	view, err := RequestView(n, "s")
	if err != nil {
		t.Fatal(err)
	}
	if !view.CanAccept || len(view.Questions) != 2 {
		t.Fatalf("form not normalized: %+v", view)
	}
	for _, values := range []map[string]json.RawMessage{
		{"project": json.RawMessage(`"c"`), "count": json.RawMessage(`2`)},
		{"project": json.RawMessage(`"a"`), "count": json.RawMessage(`4`)},
		{"project": json.RawMessage(`"a"`), "count": json.RawMessage(`1.5`)},
		{"project": json.RawMessage(`"a"`)},
		{"project": json.RawMessage(`"a"`), "count": json.RawMessage(`2`), "unexpected": json.RawMessage(`true`)},
	} {
		if _, err = ReplyFor(n, view, protocol.InteractionReply{Action: "accept", Values: values}); err == nil {
			t.Fatalf("invalid form accepted: %v", values)
		}
	}
	if _, err = ReplyFor(n, view, protocol.InteractionReply{Action: "accept", Values: map[string]json.RawMessage{"project": json.RawMessage(`"b"`), "count": json.RawMessage(`2`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedFormCanBeDeclinedButNotAccepted(t *testing.T) {
	n := Notification{Method: "mcpServer/elicitation/request", Params: json.RawMessage(`{"serverName":"demo","mode":"openaiForm","requestedSchema":{"type":"object","properties":{"nested":{"type":"object"}}}}`)}
	view, err := RequestView(n, "s")
	if err != nil {
		t.Fatal(err)
	}
	if view.CanAccept {
		t.Fatal("unsupported schema offered an accept action")
	}
	if _, err = ReplyFor(n, view, protocol.InteractionReply{Action: "accept"}); err == nil {
		t.Fatal("unsupported form accepted")
	}
	if _, err = ReplyFor(n, view, protocol.InteractionReply{Action: "decline"}); err != nil {
		t.Fatal(err)
	}
}
