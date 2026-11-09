package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/inputbridge"
	"mewdex/internal/protocol"
)

func readConnection(path string) (inputbridge.Options, error) {
	var options inputbridge.Options
	file, err := os.Open(path)
	if err != nil {
		return options, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 64<<10 {
		return options, errors.New("invalid connector connection file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&options) != nil {
		return options, errors.New("invalid connector connection file")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return options, errors.New("invalid connector connection file")
	}
	return options, nil
}

// Only the issued input key is saved. Passwords stay on the setup input stream.
func configure(ctx context.Context, path, base, name string, input io.Reader) error {
	if path == "" {
		return errors.New("provide -config for the standalone connector")
	}
	if err := config.ValidateURL(base, false); err != nil {
		return err
	}
	prior, err := readConnection(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	base = strings.TrimRight(base, "/")
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("Hub redirects are disabled")
	}}
	request, _ := http.NewRequestWithContext(ctx, "GET", base+"/health", nil)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("Hub identity check unavailable")
	}
	var server protocol.ServerInfo
	err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&server)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || server.ProtocolVersion != protocol.Version || server.HubDeviceID == "" || prior.HubDeviceID != "" && prior.HubDeviceID != server.HubDeviceID {
		return errors.New("Hub identity changed or is incompatible")
	}
	var login protocol.MewDexAccountLogin
	decoder := json.NewDecoder(io.LimitReader(input, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&login) != nil || login.Username == "" || login.Password == "" {
		return errors.New("provide MewDex username and password as JSON on standard input")
	}
	request, _ = http.NewRequestWithContext(ctx, "POST", base+"/v1/login", bytes.NewReader(protocol.JSON(login)))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		return errors.New("Hub account authorization unavailable")
	}
	var credentials struct {
		Token       string            `json:"token"`
		Identity    protocol.Identity `json:"identity"`
		HubDeviceID string            `json:"hubDeviceId"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&credentials)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || credentials.Token == "" || credentials.Identity.Role != "admin" || credentials.Identity.ID == "" || credentials.HubDeviceID != server.HubDeviceID {
		return errors.New("Hub did not authorize an administrator connector")
	}
	if prior.IdentityID != "" && prior.IdentityID != credentials.Identity.ID {
		return errors.New("use a separate connection file for another account")
	}
	if name == "" {
		name = prior.Name
	}
	options := inputbridge.Options{BaseURL: base, Token: credentials.Token, HubDeviceID: server.HubDeviceID, IdentityID: credentials.Identity.ID, Name: name}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".channel-connection-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(options)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
