package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"mewdex/internal/protocol"
)

func decodePermissionObject(raw json.RawMessage, out any) error {
	if len(raw) == 0 || len(raw) > 64<<10 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("权限请求为空或过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errors.New("权限请求包含不支持的字段")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("权限请求格式无效")
	}
	return nil
}

// RequestedPermissions only understands bounded literal filesystem additions
// and a network switch. Symbolic roots, globs and future fields are declined.
func RequestedPermissions(n Notification) (protocol.PermissionGrant, error) {
	grant := protocol.PermissionGrant{ReadPaths: []string{}, WritePaths: []string{}}
	var request struct {
		Permissions   json.RawMessage `json:"permissions"`
		EnvironmentID *string         `json:"environmentId"`
	}
	if n.Method != "item/permissions/requestApproval" || json.Unmarshal(n.Params, &request) != nil {
		return grant, errors.New("不是受支持的回合权限请求")
	}
	// The pinned engine names its native execution environment "local".
	// MewDex routes other computers to their own executor, not engine attachments.
	if request.EnvironmentID != nil && *request.EnvironmentID != "" && *request.EnvironmentID != "local" {
		return grant, errors.New("只能授权此对话当前的本机执行环境")
	}
	return parsePermissionGrant(request.Permissions)
}

func parsePermissionGrant(raw json.RawMessage) (protocol.PermissionGrant, error) {
	grant := protocol.PermissionGrant{ReadPaths: []string{}, WritePaths: []string{}}
	var permissions struct {
		FileSystem *struct {
			GlobScanMaxDepth *uint    `json:"globScanMaxDepth"`
			Read             []string `json:"read"`
			Write            []string `json:"write"`
			Entries          []struct {
				Access string `json:"access"`
				Path   struct {
					Type string `json:"type"`
					Path string `json:"path"`
				} `json:"path"`
			} `json:"entries"`
		} `json:"fileSystem"`
		Network *struct {
			Enabled *bool `json:"enabled"`
		} `json:"network"`
	}
	if err := decodePermissionObject(raw, &permissions); err != nil {
		return grant, err
	}
	if permissions.FileSystem != nil {
		if permissions.FileSystem.GlobScanMaxDepth != nil {
			return grant, errors.New("权限请求不能包含通配符扫描设置")
		}
		grant.ReadPaths, grant.WritePaths = permissions.FileSystem.Read, permissions.FileSystem.Write
		for _, entry := range permissions.FileSystem.Entries {
			if entry.Path.Type != "path" || entry.Path.Path == "" {
				return grant, errors.New("权限请求需要具体路径，不能使用通配符或系统根标记")
			}
			switch entry.Access {
			case "read":
				grant.ReadPaths = append(grant.ReadPaths, entry.Path.Path)
			case "write":
				grant.WritePaths = append(grant.WritePaths, entry.Path.Path)
			default:
				return grant, errors.New("权限请求包含不支持的访问方式")
			}
		}
	}
	if permissions.Network != nil && permissions.Network.Enabled != nil {
		grant.Network = *permissions.Network.Enabled
	}
	if len(grant.ReadPaths)+len(grant.WritePaths) > 64 {
		return grant, errors.New("一次权限请求最多包含 64 个路径")
	}
	return grant, nil
}

// CommandPermissions accepts a one-command decision only. The executor must
// additionally confirm the active profile has non-escalatable denied reads:
// in the pinned adapter those prevent both first-attempt and retry bypasses.
func CommandPermissions(n Notification) (*protocol.PermissionGrant, error) {
	var request struct {
		Kind                  string            `json:"kind"`
		Command               string            `json:"command"`
		Cwd                   string            `json:"cwd"`
		EnvironmentID         *string           `json:"environmentId"`
		AvailableDecisions    []json.RawMessage `json:"availableDecisions"`
		AdditionalPermissions json.RawMessage   `json:"additionalPermissions"`
		NetworkContext        json.RawMessage   `json:"networkApprovalContext"`
	}
	if n.Method != "item/commandExecution/requestApproval" || json.Unmarshal(n.Params, &request) != nil || request.Command == "" || request.Cwd == "" || request.Kind != "" && request.Kind != "command" {
		return nil, errors.New("命令请求缺少可核对的命令或执行目录")
	}
	if request.EnvironmentID != nil && *request.EnvironmentID != "" && *request.EnvironmentID != "local" {
		return nil, errors.New("只能确认当前本机执行环境中的命令")
	}
	if len(request.NetworkContext) != 0 && string(request.NetworkContext) != "null" {
		return nil, errors.New("请先通过本回合权限请求授权所需网络访问")
	}
	if request.AvailableDecisions != nil {
		accept := false
		for _, raw := range request.AvailableDecisions {
			if string(raw) == `"accept"` {
				accept = true
			}
		}
		if !accept {
			return nil, errors.New("引擎没有提供仅确认这一次命令的选项")
		}
	}
	if len(request.AdditionalPermissions) == 0 || string(request.AdditionalPermissions) == "null" {
		return nil, nil
	}
	grant, err := parsePermissionGrant(request.AdditionalPermissions)
	if err == nil && len(grant.ReadPaths)+len(grant.WritePaths) == 0 && !grant.Network {
		return nil, nil
	}
	return &grant, err
}

func permissionReply(grant protocol.PermissionGrant) json.RawMessage {
	permissions := map[string]any{}
	if len(grant.ReadPaths)+len(grant.WritePaths) > 0 {
		permissions["fileSystem"] = map[string]any{"read": grant.ReadPaths, "write": grant.WritePaths}
	}
	if grant.Network {
		permissions["network"] = map[string]bool{"enabled": true}
	}
	return protocol.JSON(map[string]any{"permissions": permissions, "scope": "turn"})
}
