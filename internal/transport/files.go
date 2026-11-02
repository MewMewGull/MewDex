package transport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

func FilePublisher(c config.Config) func(context.Context, protocol.Command, string, protocol.Attachment, io.Reader) (protocol.OutputFile, error) {
	return func(ctx context.Context, command protocol.Command, path string, meta protocol.Attachment, body io.Reader) (protocol.OutputFile, error) {
		var result protocol.OutputFile
		if err := config.ValidateURL(c.HubURL, c.AllowPrivateHTTP); err != nil {
			return result, err
		}
		token, err := os.ReadFile(c.NodeTokenFile)
		if err != nil {
			return result, errors.New("cannot read executor connection credential")
		}
		request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.HubURL, "/")+"/v1/file-exports?commandId="+url.QueryEscape(command.ID), body)
		if err != nil {
			return result, err
		}
		request.ContentLength = meta.Size
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Mewdex-Output", base64.RawURLEncoding.EncodeToString(protocol.JSON(protocol.FileExportRequest{Path: path, Attachment: meta})))
		response, err := hubHTTP.Do(request)
		if err != nil {
			return result, &attachments.UnconfirmedDelivery{Cause: errors.New("check the original export receipt or snapshot list before another copy")}
		}
		defer response.Body.Close()
		if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
			return result, &attachments.UnconfirmedDelivery{Cause: errors.New("Hub could not confirm snapshot storage")}
		}
		if response.StatusCode != 201 {
			return result, errors.New("Hub did not accept the file snapshot")
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result)
		if err != nil {
			return result, &attachments.UnconfirmedDelivery{Cause: err}
		}
		return result, err
	}
}
