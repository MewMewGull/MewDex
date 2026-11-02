package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

var localHTTP = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
	return errors.New("paired Hub redirects are not supported")
}}

type hubHTTPError struct{ status int }

func (e *hubHTTPError) Error() string {
	return fmt.Sprintf("MewDex Hub rejected the request (HTTP %d)", e.status)
}

func isHubUnauthorized(err error) bool {
	var status *hubHTTPError
	return errors.As(err, &status) && status.status == http.StatusUnauthorized
}

func (m *Manager) api(ctx context.Context, base, path string, body any, token string, output any) error {
	if err := config.ValidateURL(base, false); err != nil {
		return err
	}
	endpoint, _ := url.Parse(base)
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return errors.New("Hub API address requires HTTP or HTTPS")
	}
	method := "GET"
	var input io.Reader
	if body != nil {
		method = "POST"
		input = bytes.NewReader(protocol.JSON(body))
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, input)
	if err != nil {
		return err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := localHTTP.Do(request)
	if err != nil {
		return errors.New("MewDex Hub could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &hubHTTPError{status: response.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(output)
}

func listenURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (m *Manager) connect(ctx context.Context) (Result, error) {
	c, err := config.Read(m.profile())
	if err != nil {
		return Result{}, err
	}
	if !c.Hub {
		return Result{}, errors.New("use an input invitation to connect to this computer's remote Hub")
	}
	snapshot, err := m.status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !snapshot.Ready {
		return Result{}, errors.New("local Hub is not ready")
	}
	u, err := url.Parse(snapshot.BaseURL)
	if err != nil {
		return Result{}, err
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return Result{}, errors.New("automatic local pairing requires a loopback Hub")
	}
	var health struct {
		Name    string `json:"name"`
		Version int    `json:"protocolVersion"`
	}
	if err = m.api(ctx, snapshot.BaseURL, "/health", nil, "", &health); err != nil {
		return Result{}, err
	}
	if health.Name != "MewDex" || health.Version != protocol.Version {
		return Result{}, errors.New("local endpoint does not match the MewDex protocol")
	}
	s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		return Result{}, err
	}
	if token, actor, accountErr := s.LocalMewDexKey(); accountErr == nil {
		s.Close()
		var verified protocol.Identity
		if err = m.api(ctx, snapshot.BaseURL, "/v1/me", nil, token, &verified); err != nil {
			return Result{}, err
		}
		if verified.ID != actor.ID {
			return Result{}, errors.New("local MewDex account changed")
		}
		return Result{Snapshot: snapshot, Connection: &Connection{BaseURL: snapshot.BaseURL, Credentials: Credentials{Token: token, Identity: verified, ProtocolVersion: protocol.Version}}}, nil
	} else if !errors.Is(accountErr, store.ErrNotFound) {
		s.Close()
		return Result{}, accountErr
	}
	code, err := s.Invite("admin", time.Minute)
	s.Close()
	if err != nil {
		return Result{}, err
	}
	var credentials Credentials
	if err = m.api(ctx, snapshot.BaseURL, "/v1/pair", map[string]string{"code": code, "name": c.Name + " · 输入端"}, "", &credentials); err != nil {
		return Result{}, err
	}
	if credentials.ProtocolVersion != protocol.Version || credentials.Identity.Role != "admin" || credentials.Token == "" {
		return Result{}, errors.New("local Hub did not return its administrator input identity")
	}
	return Result{Snapshot: snapshot, Connection: &Connection{BaseURL: snapshot.BaseURL, Credentials: credentials}}, nil
}
