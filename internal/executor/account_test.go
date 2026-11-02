package executor

import (
	"context"
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestModelAccountReadExposesProfileWithoutCredentials(t *testing.T) {
	x, _, fake := setup(t)
	fake.mu.Lock()
	fake.account = map[string]any{"type": "chatgpt", "planType": "pro", "email": "private@example.invalid", "accessToken": "private-token"}
	fake.mu.Unlock()
	result := x.Handle(context.Background(), protocol.Command{ID: "read-account", DeviceID: "device", Kind: "account.read", SourceID: "admin"})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	if strings.Contains(string(result.Data), "private-token") || strings.Contains(string(result.Data), "accessToken") {
		t.Fatalf("private account fields escaped executor: %s", result.Data)
	}
	if !strings.Contains(string(result.Data), `"email":"private@example.invalid"`) {
		t.Fatal("account menu profile missing")
	}
	if !strings.Contains(string(result.Data), `"signedIn":true`) {
		t.Fatal("account state missing")
	}
}

func TestGlobalAccountCannotBeReplacedByRemoteLogin(t *testing.T) {
	x, _, fake := setup(t)
	result := x.Handle(context.Background(), protocol.Command{ID: "login", DeviceID: "device", Kind: "account.login", SourceID: "admin"})
	if result.State != "failed" {
		t.Fatal("existing global authentication could be replaced")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "account/login/start" {
			t.Fatal("login was sent to a non-isolated engine")
		}
	}
}
