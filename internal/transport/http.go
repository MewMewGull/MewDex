package transport

import (
	"errors"
	"net/http"
)

// A network endpoint is paired explicitly. Neither device credentials nor
// attachment requests follow an endpoint's redirects to a different service.
var hubHTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
	return errors.New("Hub redirects are not supported")
}}
