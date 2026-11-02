package executor

import (
	"errors"
	"fmt"
	"path/filepath"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/workspace"
)

type projectBinding struct {
	ID       string `json:"id"`
	DeviceID string `json:"deviceId"`
	Path     string `json:"path"`
}

func (x *Executor) bindWorkspaces() error {
	return x.prepareWorkspaces(true)
}

// ValidateWorkspaceConfiguration is shared with stopped-service configuration
// management. It checks durable bindings without reserving new project IDs.
func ValidateWorkspaceConfiguration(c config.Config, s *store.Store) error {
	x := &Executor{config: c, store: s, projectRoots: map[string]string{}}
	return x.prepareWorkspaces(false)
}

func (x *Executor) prepareWorkspaces(persist bool) error {
	dynamic, err := store.List[protocol.Project](x.store, "workspaces")
	if err != nil {
		return err
	}
	x.config.Projects = append([]protocol.Project(nil), x.config.Projects...)
	for _, project := range dynamic {
		found := false
		for i, configured := range x.config.Projects {
			if configured.ID == project.ID {
				x.config.Projects[i].Name, x.config.Projects[i].Category = project.Name, project.Category
				found = true
				break
			}
		}
		if !found {
			x.config.Projects = append(x.config.Projects, project)
		}
	}
	bindings, err := store.List[projectBinding](x.store, "project_bindings")
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.DeviceID != x.config.DeviceID || binding.ID == "" || !filepath.IsAbs(binding.Path) {
			return errors.New("executor data contains a different device or an invalid workspace binding")
		}
		x.projectRoots[binding.ID] = binding.Path
	}
	x.config.Projects = append([]protocol.Project(nil), x.config.Projects...)
	seen := map[string]bool{}
	// Validate every entry before creating new bindings.
	for i, project := range x.config.Projects {
		if project.ID == "" || seen[project.ID] {
			return errors.New("project IDs must be unique and non-empty")
		}
		seen[project.ID] = true
		root, err := workspace.CanonicalDir(project.Path)
		if err != nil {
			return fmt.Errorf("project %s: %w", project.ID, err)
		}
		if old := x.projectRoots[project.ID]; old != "" && old != root {
			return fmt.Errorf("project %s is bound to another directory; use a new project ID for a different workspace", project.ID)
		}
		x.config.Projects[i].Path = root
	}
	for _, project := range x.config.Projects {
		if x.projectRoots[project.ID] == "" {
			if persist {
				if err = x.store.Put("project_bindings", project.ID, projectBinding{ID: project.ID, DeviceID: x.config.DeviceID, Path: project.Path}); err != nil {
					return err
				}
			}
			x.projectRoots[project.ID] = project.Path
		}
	}
	return nil
}

func (x *Executor) claimProject(s *protocol.Session) error {
	project, err := x.project(s.ProjectID)
	if err != nil {
		return err
	}
	root, err := workspace.CanonicalDir(project.Path)
	if err != nil {
		return err
	}
	if root != x.projectRoot(s.ProjectID) {
		return errors.New("project directory changed; review its configuration before executing")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for session, occupied := range x.workspaceOwners {
		if session != s.ID && workspace.Overlap(root, occupied) {
			return errors.New("workspace overlaps another active or unconfirmed task")
		}
	}
	x.workspaceOwners[s.ID] = root
	return nil
}

func (x *Executor) restoreProjectClaim(s protocol.Session) {
	x.mu.Lock()
	x.workspaceOwners[s.ID] = x.projectRoot(s.ProjectID)
	x.mu.Unlock()
}
