package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/webui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "MewDex Web:", err)
		os.Exit(1)
	}
}

func run() error {
	program, err := os.Executable()
	if err != nil {
		return err
	}
	listen := flag.String("listen", "127.0.0.1:47831", "web input listener")
	directory := flag.String("ui", filepath.Join(filepath.Dir(program), "ui"), "standalone input assets")
	hubURL := flag.String("hub-url", "", "Hub HTTP(S) endpoint")
	private := flag.Bool("allow-private-http", false, "allow a private development Hub over HTTP")
	flag.Parse()
	if err = config.ValidateURL(*hubURL, *private); err != nil {
		return err
	}
	target, err := url.Parse(*hubURL)
	if err != nil || target.Scheme != "http" && target.Scheme != "https" {
		return errors.New("use an HTTP(S) Hub endpoint")
	}
	if _, err = os.Stat(filepath.Join(*directory, "index.html")); err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	// The proxy has one operator-configured upstream, including WebSocket upgrades.
	// Browser credentials remain scoped to the user's web session.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Hub unavailable", http.StatusBadGateway)
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/", proxy)
	mux.Handle("/health", proxy)
	mux.Handle("/", webui.Handler(*directory))
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Println("MewDex Web listening:", *listen)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
