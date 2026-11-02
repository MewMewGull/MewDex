package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

// AuthenticatedProxyURL is used only for the child environment or HTTP transport.
// Callers must not log its result, which may contain a local proxy credential.
func (c Engine) AuthenticatedProxyURL() (string, error) {
	if c.ProxyURL == "" {
		return "", errors.New("engine proxy is not configured")
	}
	if err := ValidateURL(c.ProxyURL, true); err != nil {
		return "", err
	}
	u, _ := url.Parse(c.ProxyURL)
	credential := c.ProxyCredential
	if credential == "" && c.ProxyTokenFile != "" {
		raw, err := os.ReadFile(c.ProxyTokenFile)
		if err != nil {
			return "", errors.New("cannot read proxy credential file")
		}
		credential = strings.TrimSpace(string(raw))
		if credential == "" {
			return "", errors.New("proxy credential is empty")
		}
	}
	if credential != "" {
		u.User = url.UserPassword(c.ProxyUsername, credential)
	}
	return u.String(), nil
}
