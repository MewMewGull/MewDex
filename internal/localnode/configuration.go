package localnode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
	"mewdex/internal/workspace"
)

func normalizeProjects(input []protocol.Project) ([]protocol.Project, error) {
	output := make([]protocol.Project, 0, len(input))
	seen := map[string]bool{}
	for _, project := range input {
		root, err := workspace.CanonicalDir(project.Path)
		if err != nil {
			return nil, err
		}
		project.Path = root
		if project.ID == "" {
			digest := sha256.Sum256([]byte(root))
			project.ID = hex.EncodeToString(digest[:12])
		}
		if seen[project.ID] {
			return nil, errors.New("project IDs must be unique")
		}
		seen[project.ID] = true
		if strings.TrimSpace(project.Name) == "" {
			project.Name = filepath.Base(root)
		}
		output = append(output, project)
	}
	return output, nil
}

func (m *Manager) setup(ctx context.Context, setup *Setup) error {
	if setup == nil {
		return errors.New("local service settings are required")
	}
	if _, err := os.Stat(m.profile()); err == nil {
		return errors.New("a local device is already configured; its data was preserved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !setup.Hub && !setup.Executor {
		return errors.New("select a Hub or executor role for this computer")
	}
	projects, err := normalizeProjects(setup.Projects)
	if err != nil {
		return err
	}
	name := setup.Name
	if strings.TrimSpace(name) == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = "MewDex"
	}
	listen := setup.Listen
	if listen == "" {
		listen = "127.0.0.1:47830"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return errors.New("invalid local Hub listener")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 || (host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())) {
		return errors.New("desktop setup requires a loopback Hub listener")
	}
	if setup.Executor || setup.Hub {
		if !filepath.IsAbs(m.options.Engine) {
			return errors.New("a bundled execution engine is required")
		}
		info, err := os.Stat(m.options.Engine)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("bundled execution engine is missing")
		}
	}
	enginePath := m.options.Engine
	if setup.Executor || setup.Hub {
		kind, kindErr := runtimefiles.EngineKind(m.options.Engine)
		if kindErr != nil {
			return kindErr
		}
		if setup.Hub && kind != "codex" {
			return errors.New("a Hub requires its bundled model runtime")
		}
		enginePath, err = runtimefiles.Engine(m.options.Root, m.options.Engine, engine.CompatibleVersion)
		if err != nil {
			return err
		}
	}
	c := config.Config{DeviceID: protocol.ID(), Name: name, DataDir: filepath.Join(m.options.Root, "data"), Listen: listen, Hub: setup.Hub, Executor: setup.Executor, HubURL: setup.HubURL, Projects: projects,
		AllowedOrigins: []string{"tauri://localhost", "http://tauri.localhost", "https://tauri.localhost", "http://127.0.0.1:1420", "http://localhost:1420"},
		Engine:         config.Engine{Executable: enginePath, Home: filepath.Join(m.options.Root, "engine-home")}}
	if c.Hub {
		c.AllowedOrigins = append(c.AllowedOrigins, "http://127.0.0.1:47831", "http://localhost:47831")
	}
	if !c.Hub {
		if err := config.ValidateURL(c.HubURL, false); err != nil {
			return err
		}
		c.NodeTokenFile = filepath.Join(m.options.Root, "node-token")
	} else if c.HubURL != "" {
		return errors.New("a new Hub does not join a second Hub")
	}
	if pending, err := config.Read(m.draft()); err == nil {
		c.DeviceID = pending.DeviceID
		c.Engine.ProxyUsername = c.DeviceID
		pending.Engine.UseHubGateway, pending.Gateway = false, nil
		if !bytes.Equal(protocol.JSON(c), protocol.JSON(pending)) {
			return errors.New("an unfinished setup exists with different settings; it was preserved")
		}
		if err = replaceJSON(m.draft(), c); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		c.Engine.ProxyUsername = c.DeviceID
		if err := createJSON(m.draft(), c); err != nil {
			return err
		}
	} else {
		return errors.New("unfinished local setup cannot be read; its files were preserved")
	}
	if !c.Hub {
		if err = m.pairExecutor(ctx, c, setup.Invitation); err != nil {
			return err
		}
	}
	if setup.Hub && setup.Account != nil {
		s, openErr := store.Open(filepath.Join(c.DataDir, "hub.db"))
		if openErr != nil {
			return openErr
		}
		accountErr := s.ProvisionMewDexAccount(*setup.Account)
		s.Close()
		if accountErr != nil {
			return accountErr
		}
	}
	if err := createJSON(m.profile(), c); err != nil {
		return err
	}
	preference, err := m.StartupPreference()
	if err != nil {
		return err
	}
	if preference != nil {
		_, err = m.applyStartup(ctx, &StartupRequest{Enabled: *preference})
	}
	return err
}

func createJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return createFile(path, append(raw, '\n'))
}
func createFile(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(raw); err != nil {
		return err
	}
	return file.Sync()
}

func (m *Manager) projects(projects []protocol.Project) error {
	c, err := config.Read(m.profile())
	if err != nil {
		return err
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return errors.New("stop the local service before changing execution projects")
	}
	defer lock.Close()
	resolved, err := normalizeProjects(projects)
	if err != nil {
		return err
	}
	// Existing IDs retain their directory, including before their first task.
	for _, next := range resolved {
		for _, old := range c.Projects {
			if next.ID == old.ID && next.Path != old.Path {
				return errors.New("use a new project ID for a different directory")
			}
		}
	}
	c.Projects = resolved
	if _, statErr := os.Stat(filepath.Join(c.DataDir, "executor.db")); statErr == nil {
		s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
		if err != nil {
			return err
		}
		err = executor.ValidateWorkspaceConfiguration(c, s)
		s.Close()
		if err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	return replaceJSON(m.profile(), c)
}

func replaceJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("local profile must be a regular file")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	temp := path + "." + protocol.ID() + ".tmp"
	if err = createJSON(temp, value); err != nil {
		return err
	}
	defer os.Remove(temp)
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("local configuration changed while it was being saved")
	}
	return os.Rename(temp, path)
}
