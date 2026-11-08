package hub

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (h *Hub) projectsAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/")
	if len(parts) == 2 && parts[1] == "computer" {
		h.computerAPI(w, r, parts[0])
		return
	}
	if len(parts) >= 2 && parts[1] == "harness" {
		h.projectHarnessAPI(w, r, parts)
		return
	}
	if identity(r).Role != "admin" {
		problem(w, 403, errors.New("只有管理员可以管理 Hub 项目"))
		return
	}
	if r.Method == "GET" && r.URL.Path == "/v1/projects" {
		projects, err := h.store.HubProjects()
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, slices.DeleteFunc(projects, func(project protocol.Project) bool { return project.Removed }))
		return
	}
	if len(parts) != 1 || !strings.HasPrefix(r.URL.Path, "/v1/projects/hp_") || (r.Method != "PUT" && r.Method != "DELETE") {
		problem(w, 405, errors.New("unsupported Hub project operation"))
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
	if r.Method == "DELETE" {
		revision, err := strconv.ParseInt(r.URL.Query().Get("expectedRevision"), 10, 64)
		if err != nil || revision < 0 {
			problem(w, 400, errors.New("请提供有效的项目版本"))
			return
		}
		h.projectMu.Lock()
		project, err := h.store.RemoveHubProject(h.config.DeviceID, id, revision)
		h.projectMu.Unlock()
		if err != nil {
			status := 500
			if errors.Is(err, store.ErrNotFound) {
				status, err = 404, errors.New("项目已不可用，请刷新列表")
			} else if errors.Is(err, store.ErrConflict) {
				status, err = 409, errors.New("项目已在其他输入端修改，请刷新后重试")
			}
			problem(w, status, err)
			return
		}
		h.notify()
		reply(w, 200, project)
		return
	}
	var input protocol.HubProjectRequest
	if !decode(w, r, &input) {
		return
	}
	h.projectMu.Lock()
	defer h.projectMu.Unlock()
	var prior protocol.Project
	if h.store.Get("hub_projects", id, &prior) == nil && !slices.EqualFunc(prior.Directories, input.Directories, func(a, b protocol.ProjectDirectory) bool {
		return a.DeviceID == b.DeviceID && a.ProjectID == b.ProjectID
	}) {
		sessions, err := store.List[protocol.Session](h.store, "sessions")
		if err != nil {
			problem(w, 500, err)
			return
		}
		for _, session := range sessions {
			if session.DeviceID == h.coordinatorID && session.ProjectID == id && session.ExecutionUnconfirmed() {
				problem(w, 409, errors.New("项目仍有执行中或结果未确认的任务，请先结束任务再调整目录组"))
				return
			}
		}
	}
	project, err := h.store.SaveHubProject(h.config.DeviceID, id, input)
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrConflict) {
			status = 409
			err = errors.New("项目已在其他输入端修改，请刷新后重试")
		} else if errors.Is(err, store.ErrNotFound) {
			status, err = 404, errors.New("项目已移除，请刷新列表")
		}
		problem(w, status, err)
		return
	}
	h.notify()
	reply(w, 200, project)
}
