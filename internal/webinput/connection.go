// Package webinput owns a standalone web input's background channel connections.
package webinput

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mewdex/internal/inputbridge"
)

type Channels struct {
	root       string
	ctx        context.Context
	mu         sync.Mutex
	connection inputbridge.Options
	cancel     context.CancelFunc
	done       chan struct{}
}

func New(ctx context.Context, root string) (*Channels, error) {
	c := &Channels{root: root, ctx: ctx}
	raw, err := os.ReadFile(filepath.Join(root, "channel-connection"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	raw, err = unprotect(raw)
	if err != nil {
		return nil, errors.New("cannot unlock the web input channel connection")
	}
	if json.Unmarshal(raw, &c.connection) != nil {
		return nil, errors.New("cannot read the web input channel connection")
	}
	c.start()
	return c, nil
}

func (c *Channels) start() {
	ctx, cancel := context.WithCancel(c.ctx)
	c.cancel, c.done = cancel, make(chan struct{})
	options, done := c.connection, c.done
	options.Root = filepath.Join(c.root, "channels")
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			_ = inputbridge.Run(ctx, options)
			timer := time.NewTimer(3 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}()
}

func (c *Channels) stop() error {
	if c.cancel == nil {
		return nil
	}
	c.cancel()
	select {
	case <-c.done:
		c.cancel, c.done = nil, nil
		return nil
	case <-time.After(20 * time.Second):
		return errors.New("the previous channel connection is still closing")
	}
}

func (c *Channels) Configure(ctx context.Context, options inputbridge.Options) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return errors.New("the web input is closing")
	}
	if c.connection.Token != "" && (c.connection.HubDeviceID != options.HubDeviceID || c.connection.IdentityID != options.IdentityID || c.connection.BaseURL != options.BaseURL) {
		return errors.New("sign out the currently connected web input account before connecting another")
	}
	if c.cancel != nil && c.connection == options {
		return nil
	}
	validation := options
	validation.Root = filepath.Join(c.root, "channels")
	if err := inputbridge.ValidateConnection(ctx, validation); err != nil {
		return err
	}
	if err := c.stop(); err != nil {
		return err
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return err
	}
	raw, err = protect(raw)
	if err != nil {
		return err
	}
	path := filepath.Join(c.root, "channel-connection")
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	c.connection = options
	c.start()
	return nil
}

func (c *Channels) Disconnect(token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connection.Token == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(c.connection.Token)) != 1 {
		return errors.New("the connected account's credential is required")
	}
	if err := c.stop(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(c.root, "channel-connection")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	c.connection = inputbridge.Options{}
	return nil
}

func (c *Channels) Close() error { c.mu.Lock(); defer c.mu.Unlock(); return c.stop() }

func (c *Channels) Connected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.cancel != nil }

func (c *Channels) Connection() inputbridge.Options {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connection
}
