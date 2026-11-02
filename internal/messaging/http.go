package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type platformError struct {
	Status int
	Code   int
}

func (e *platformError) Error() string {
	return fmt.Sprintf("platform HTTP %d (code %d)", e.Status, e.Code)
}

func platformJSON(ctx context.Context, client *http.Client, method, endpoint string, headers http.Header, body any, output any) error {
	var data []byte
	var err error
	if body != nil {
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid platform endpoint")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = http.Header{}
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		// A transport error can contain a credential-bearing URL. Keep it out
		// of channel snapshots, platform replies and service logs.
		return fmt.Errorf("platform request failed")
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return fmt.Errorf("invalid platform response size")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(data, &failure)
		return &platformError{Status: response.StatusCode, Code: failure.Code}
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return fmt.Errorf("invalid platform JSON response")
	}
	return nil
}
