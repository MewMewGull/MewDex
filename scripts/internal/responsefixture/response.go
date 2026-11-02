// Package responsefixture provides the shared, deterministic loopback provider
// used by native-engine risk probes. It never contacts a model service.
package responsefixture

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mewdex/internal/protocol"
)

// A loopback Responses fixture emits fixed tool calls, never inference. Its
// requests contain only this probe's synthetic conversation and owned paths.
type Fixture struct {
	mu              sync.Mutex
	directory       string
	command         string
	permissions     map[string]any
	commandApproval bool
	filePatch       string
	requests        int
	round           int
	roundRequests   int
	tool            string
	namespace       string
	error           string
}

func New(directory string) *Fixture { return &Fixture{directory: directory} }

type Snapshot struct {
	Requests  int    `json:"requests"`
	Tool      string `json:"tool"`
	Namespace string `json:"namespace"`
	Error     string `json:"error"`
}

func (f *Fixture) Snapshot() Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Snapshot{Requests: f.requests, Tool: f.tool, Namespace: f.namespace, Error: f.error}
}

func (f *Fixture) begin(command string) {
	f.command = command
	f.permissions = nil
	f.commandApproval = false
	f.filePatch = ""
	f.round++
	f.roundRequests = 0
}

func (f *Fixture) Begin(command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begin(command)
}

func (f *Fixture) BeginPermission(command string, permissions map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begin(command)
	f.permissions = permissions
}

func (f *Fixture) BeginCommandApproval(command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begin(command)
	f.commandApproval = true
}

func (f *Fixture) BeginPatch(command, patch string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begin(command)
	f.filePatch = patch
}

func (f *Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/v1/responses" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "" {
		http.Error(w, "fixture refuses authentication", http.StatusForbidden)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20+1))
	if err != nil || len(raw) > 2<<20 {
		http.Error(w, "invalid fixture request size", http.StatusBadRequest)
		return
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(raw, &request) != nil {
		http.Error(w, "invalid fixture request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	index := f.requests
	f.roundRequests++
	roundRequest, round := f.roundRequests, f.round
	if err = os.WriteFile(filepath.Join(f.directory, fmt.Sprintf("request-%02d.json", index)), raw, 0600); err != nil {
		f.error = err.Error()
		http.Error(w, "cannot retain fixture request", 500)
		return
	}
	if roundRequest > 3 || index > 20 {
		f.error = "fixture received unexpected repeated requests"
		http.Error(w, f.error, 409)
		return
	}
	var tools []map[string]json.RawMessage
	_ = json.Unmarshal(request["tools"], &tools)
	var input []struct {
		Type  string                       `json:"type"`
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(request["input"], &input)
	for _, item := range input {
		if item.Type == "additional_tools" {
			tools = append(tools, item.Tools...)
		}
	}
	if roundRequest == 1 {
		f.tool, f.namespace = findShell(tools, "")
		if f.tool == "" {
			f.tool, f.namespace = findTool(tools, "", []string{"exec"})
		}
		if f.tool == "" {
			f.error = "the pinned engine did not advertise a supported shell tool"
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rid := fmt.Sprintf("resp_mewdex_fixture_%d", index)
	response := map[string]any{"id": rid, "object": "response", "created_at": time.Now().Unix(), "status": "in_progress", "model": "gpt-6.1-sol", "output": []any{}}
	sequence := 0
	emit := func(kind string, values map[string]any) {
		values["type"], values["sequence_number"] = kind, sequence
		sequence++
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, protocol.JSON(values))
		if flush, ok := w.(http.Flusher); ok {
			flush.Flush()
		}
	}
	emit("response.created", map[string]any{"response": response})
	var item map[string]any
	permissionCall := f.permissions != nil && roundRequest == 1
	shellCall := roundRequest == 1 || f.permissions != nil && roundRequest == 2
	if shellCall && f.tool != "" {
		var arguments any
		tool, namespace := f.tool, f.namespace
		orchestrated := tool == "exec"
		innerTool := "exec_command"
		if permissionCall {
			innerTool = "request_permissions"
			if !orchestrated {
				tool, namespace = findTool(tools, "", []string{"request_permissions"})
			}
			if tool == "" {
				f.error = "the pinned engine did not advertise request_permissions"
				http.Error(w, f.error, 409)
				return
			}
			arguments = map[string]any{"permissions": f.permissions, "reason": "Authorize only this owned synthetic marker for the current turn."}
		} else {
			switch tool {
			case "exec":
				arguments = map[string]any{"cmd": f.command, "yield_time_ms": 1000, "max_output_tokens": 2000}
			case "exec_command":
				arguments = map[string]any{"cmd": f.command, "login": false, "yield_time_ms": 1000, "max_output_tokens": 2000}
			case "shell_command":
				arguments = map[string]any{"command": f.command, "login": false, "timeout_ms": 10000}
			}
			if f.commandApproval {
				arguments.(map[string]any)["sandbox_permissions"] = "require_escalated"
				arguments.(map[string]any)["justification"] = "Confirm only this fixed owned marker operation while retaining the active private-data boundary."
			}
		}
		if orchestrated && f.filePatch != "" {
			innerTool = "apply_patch"
			arguments = f.filePatch
		}
		text := string(protocol.JSON(arguments))
		itemType, inputField := "function_call", "arguments"
		if orchestrated {
			itemType, inputField = "custom_tool_call", "input"
			text = "const result = await tools." + innerTool + "(" + text + "); text(result);"
		}
		item = map[string]any{"id": fmt.Sprintf("fc_mewdex_fixture_%d_%d", round, roundRequest), "type": itemType, "call_id": fmt.Sprintf("call_mewdex_fixture_%d_%d", round, roundRequest), "name": tool, inputField: "", "status": "in_progress"}
		if namespace != "" {
			item["namespace"] = namespace
		}
		emit("response.output_item.added", map[string]any{"output_index": 0, "item": item})
		if !orchestrated {
			emit("response.function_call_arguments.delta", map[string]any{"output_index": 0, "item_id": item["id"], "delta": text})
			emit("response.function_call_arguments.done", map[string]any{"output_index": 0, "item_id": item["id"], "arguments": text})
		}
		item[inputField], item["status"] = text, "completed"
	} else {
		item = map[string]any{"id": fmt.Sprintf("msg_mewdex_fixture_%d", round), "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
		emit("response.output_item.added", map[string]any{"output_index": 0, "item": item})
		part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
		emit("response.content_part.added", map[string]any{"output_index": 0, "item_id": item["id"], "content_index": 0, "part": part})
		emit("response.output_text.delta", map[string]any{"output_index": 0, "item_id": item["id"], "content_index": 0, "delta": "MewDex local fixture completed."})
		part["text"] = "MewDex local fixture completed."
		emit("response.output_text.done", map[string]any{"output_index": 0, "item_id": item["id"], "content_index": 0, "text": part["text"]})
		emit("response.content_part.done", map[string]any{"output_index": 0, "item_id": item["id"], "content_index": 0, "part": part})
		item["content"], item["status"] = []any{part}, "completed"
	}
	emit("response.output_item.done", map[string]any{"output_index": 0, "item": item})
	response["status"], response["output"] = "completed", []any{item}
	response["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0, "input_tokens_details": map[string]int{"cached_tokens": 0}, "output_tokens_details": map[string]int{"reasoning_tokens": 0}}
	emit("response.completed", map[string]any{"response": response})
}

func findShell(tools []map[string]json.RawMessage, namespace string) (string, string) {
	return findTool(tools, namespace, []string{"exec_command", "shell_command"})
}

func findTool(tools []map[string]json.RawMessage, namespace string, names []string) (string, string) {
	for _, tool := range tools {
		var name, kind string
		_ = json.Unmarshal(tool["name"], &name)
		_ = json.Unmarshal(tool["type"], &kind)
		if kind == "function" || kind == "custom" {
			for _, expected := range names {
				if name == expected {
					return name, namespace
				}
			}
		}
		if kind == "namespace" {
			var nested []map[string]json.RawMessage
			_ = json.Unmarshal(tool["tools"], &nested)
			if found, ns := findTool(nested, name, names); found != "" {
				return found, ns
			}
		}
	}
	return "", ""
}
