package main

import (
	"fmt"
	"mewdex/internal/servicetray"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	if err := run(); err != nil {
		if len(os.Args) == 1 {
			servicetray.ShowError("MewDex · 网页输入端", err.Error())
		}
		fmt.Fprintln(os.Stderr, "MewDex Web:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "tray-helper":
			return servicetray.RunHelper()
		case "open-url-helper":
			return servicetray.RunOpenHelper()
		case "managed":
			return runWeb(os.Args[2:], true)
		}
	}
	return runWeb(os.Args[1:], false)
}

func newHubProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Hub unavailable", http.StatusBadGateway)
	}
	return proxy
}
