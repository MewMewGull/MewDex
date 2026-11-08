package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

// Connection accepts the existing input connection format. Credentials are
// retained in memory and never included in run state, traces or reports.
type Connection struct {
	BaseURL          string `json:"baseURL"`
	HubDeviceID      string `json:"hubDeviceId"`
	AllowPrivateHTTP bool   `json:"allowPrivateHTTP"`
	Credentials      struct {
		Token    string            `json:"token"`
		Identity protocol.Identity `json:"identity"`
	} `json:"credentials"`
}

type hubClient struct {
	connection Connection
	http       *http.Client
}

type responseError struct{ status int }

func (e *responseError) Error() string { return fmt.Sprintf("Hub request returned HTTP %d", e.status) }

func newClient(connection Connection) (*hubClient, error) {
	if err := config.ValidateURL(connection.BaseURL, connection.AllowPrivateHTTP); err != nil || !strings.HasPrefix(connection.BaseURL, "http") || connection.Credentials.Token == "" {
		return nil, errors.New("evaluation requires an HTTP(S) Hub URL and an input credential")
	}
	connection.BaseURL = strings.TrimRight(connection.BaseURL, "/")
	return &hubClient{connection: connection, http: &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *hubClient) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.connection.BaseURL+path, body)
	if err != nil {
		return errors.New("invalid evaluation Hub request")
	}
	request.Header.Set("Authorization", "Bearer "+c.connection.Credentials.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return errors.New("evaluation Hub response is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &responseError{status: response.StatusCode}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	if err = decoder.Decode(output); err != nil {
		return errors.New("evaluation Hub returned an invalid or oversized response")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("evaluation Hub returned multiple response values")
	}
	return nil
}
