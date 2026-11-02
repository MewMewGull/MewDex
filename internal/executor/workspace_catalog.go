package executor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mewdex/internal/protocol"
	"mewdex/internal/workspace"
)

func (x *Executor) Projects() []protocol.Project {
	x.projectMu.RLock()
	defer x.projectMu.RUnlock()
	return append([]protocol.Project{}, x.config.Projects...)
}
func (x *Executor) projectRoot(id string) string {
	x.projectMu.RLock()
	defer x.projectMu.RUnlock()
	return x.projectRoots[id]
}

// Workspace management is an administrator command, separate from model tools.
// Registering a directory never changes an existing conversation's binding.
func (x *Executor) workspaceCommand(c protocol.Command) protocol.CommandResult {
	request := c.Workspace
	if request == nil {
		return commandFailure(errors.New("workspace request required"))
	}
	requestedPath := request.Path
	if requestedPath == "~" || strings.HasPrefix(requestedPath, "~/") || strings.HasPrefix(requestedPath, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return commandFailure(err)
		}
		requestedPath = filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(requestedPath, "~"), "/\\"))
	}
	if c.Kind == "workspace.browse" {
		listing := protocol.WorkspaceListing{Projects: x.Projects(), Directories: []protocol.WorkspaceDirectory{}}
		if requestedPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return commandFailure(err)
			}
			listing.Directories = append(listing.Directories, protocol.WorkspaceDirectory{Name: "用户目录", Path: home})
			if runtime.GOOS == "windows" {
				for drive := 'A'; drive <= 'Z'; drive++ {
					path := string(drive) + ":\\"
					if info, err := os.Stat(path); err == nil && info.IsDir() {
						listing.Directories = append(listing.Directories, protocol.WorkspaceDirectory{Name: path, Path: path})
					}
				}
			} else {
				listing.Directories = append(listing.Directories, protocol.WorkspaceDirectory{Name: "/", Path: "/"})
			}
		} else {
			path, err := workspace.CanonicalDir(requestedPath)
			if err != nil {
				return commandFailure(err)
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return commandFailure(err)
			}
			listing.Path, listing.Parent = path, filepath.Dir(path)
			for _, entry := range entries {
				if entry.IsDir() {
					listing.Directories = append(listing.Directories, protocol.WorkspaceDirectory{Name: entry.Name(), Path: filepath.Join(path, entry.Name())})
				}
			}
		}
		return protocol.CommandResult{State: "completed", Data: protocol.JSON(listing)}
	}
	if c.Kind == "workspace.update" {
		if strings.TrimSpace(request.Name) == "" || len(request.Name) > 100 || len(request.Category) > 80 {
			return commandFailure(errors.New("请输入有效的名称和分类"))
		}
		project, err := x.updateWorkspace(c.ProjectID, request)
		if err != nil {
			return commandFailure(err)
		}
		if err := x.emit("", "workspace.catalog.updated", x.Projects()); err != nil {
			return protocol.CommandResult{State: "unknown", Error: "工作区已保存，目录同步暂未确认"}
		}
		return protocol.CommandResult{State: "completed", Data: protocol.JSON(project)}
	}
	if c.Kind != "workspace.create" {
		return commandFailure(errors.New("unsupported workspace command"))
	}
	if !filepath.IsAbs(requestedPath) || strings.TrimSpace(request.Name) == "" || len(request.Name) > 100 || len(request.Category) > 80 {
		return commandFailure(errors.New("请输入工作区名称和执行设备上的绝对目录"))
	}
	if request.CreateFolder {
		if err := os.Mkdir(requestedPath, 0755); err != nil {
			return commandFailure(err)
		}
	}
	root, err := workspace.CanonicalDir(requestedPath)
	if err != nil {
		return commandFailure(err)
	}
	if filepath.Dir(root) == root {
		return commandFailure(errors.New("请选择磁盘下的项目目录"))
	}
	project, err := x.registerWorkspace(root, request)
	if err != nil {
		return commandFailure(err)
	}
	if err := x.emit("", "workspace.catalog.updated", x.Projects()); err != nil {
		return protocol.CommandResult{State: "unknown", Error: "工作区已保存，目录同步暂未确认；请刷新工作区列表"}
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(project)}
}

func (x *Executor) updateWorkspace(id string, request *protocol.WorkspaceRequest) (protocol.Project, error) {
	x.projectMu.Lock()
	defer x.projectMu.Unlock()
	for i, project := range x.config.Projects {
		if project.ID != id {
			continue
		}
		project.Name, project.Category = strings.TrimSpace(request.Name), strings.TrimSpace(request.Category)
		if err := x.store.Put("workspaces", id, project); err != nil {
			return protocol.Project{}, err
		}
		x.config.Projects[i] = project
		return project, nil
	}
	return protocol.Project{}, errors.New("工作区已不可用，请刷新列表")
}

func (x *Executor) registerWorkspace(root string, request *protocol.WorkspaceRequest) (protocol.Project, error) {
	x.projectMu.Lock()
	defer x.projectMu.Unlock()
	for _, project := range x.config.Projects {
		if project.Path == root {
			if project.Name != strings.TrimSpace(request.Name) || project.Category != strings.TrimSpace(request.Category) {
				return protocol.Project{}, fmt.Errorf("此文件夹已添加为“%s”，可在项目菜单中修改名称与分类", project.Name)
			}
			return project, nil
		}
	}
	project := protocol.Project{ID: "p_" + protocol.ID(), Name: strings.TrimSpace(request.Name), Path: root, Category: strings.TrimSpace(request.Category)}
	if err := x.store.Put("project_bindings", project.ID, projectBinding{ID: project.ID, DeviceID: x.config.DeviceID, Path: root}); err != nil {
		return protocol.Project{}, err
	}
	if err := x.store.Put("workspaces", project.ID, project); err != nil {
		return protocol.Project{}, err
	}
	x.config.Projects = append(x.config.Projects, project)
	x.projectRoots[project.ID] = root
	return project, nil
}
