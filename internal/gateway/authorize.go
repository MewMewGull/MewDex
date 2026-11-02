package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"mewdex/internal/protocol"
)

func IdentityGrant(id protocol.Identity, device string) (Grant, error) {
	if id.Role != "executor" || id.DeviceID != device || !id.Access.Gateway {
		return Grant{}, errors.New("device is not authorized to use this gateway")
	}
	return Grant{IdentityID: id.ID, DeviceID: id.DeviceID}, nil
}

// RemoteAuthorizer lets a standalone gateway validate grants with its configured Hub.
func RemoteAuthorizer(base string) Authorizer {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, device, token string) (Grant, error) {
		var grant Grant
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/v1/gateway/authorize", bytes.NewReader(protocol.JSON(map[string]string{"deviceId": device})))
		if err != nil {
			return grant, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return grant, errors.New("gateway authorization service unavailable")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return grant, errors.New("gateway access denied")
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&grant)
		return grant, err
	}
}
