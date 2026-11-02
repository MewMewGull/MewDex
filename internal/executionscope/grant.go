package executionscope

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"mewdex/internal/protocol"
)

// HasDeniedReads is required before accepting an execution approval: the
// pinned engine keeps both command attempts and file changes sandboxed when
// an active profile contains a non-escalatable deny-read entry.
func (p Profile) HasDeniedReads() bool {
	for _, access := range p.filesystem {
		if access == "deny" {
			return true
		}
	}
	return false
}

func (p Profile) ValidateCommandDirectory(raw string) error {
	path, err := literalPath(raw)
	if err != nil {
		return errors.New("命令目录已变更或不可用")
	}
	if path != filepath.Clean(raw) {
		return errors.New("命令目录必须使用真实路径；目录别名已变化时请重新发起请求")
	}
	for root, access := range p.filesystem {
		if access == "deny" && within(root, path) {
			return errors.New("命令目录属于执行端的私有数据或身份目录")
		}
	}
	return nil
}

// Unlike a turn grant, a command decision cannot replace the engine's requested
// paths with normalized ones. Require canonical literals before approving it.
func (p Profile) ValidateCommandGrant(request protocol.PermissionGrant) (protocol.PermissionGrant, error) {
	grant, err := p.ValidateGrant(request)
	if err != nil {
		return grant, err
	}
	for _, paths := range [][]string{request.ReadPaths, request.WritePaths} {
		for _, raw := range paths {
			path, err := literalPath(raw)
			if err != nil || path != filepath.Clean(raw) {
				return grant, errors.New("命令的额外权限需要真实路径，请通过回合权限请求授权目录别名")
			}
		}
	}
	return grant, nil
}

// ValidateGrant resolves literal paths without changing the base profile.
// An explicit user grant may add access to other project material, but cannot
// expose node secrets or turn read-only runtime/input resources into writes.
func (p Profile) ValidateGrant(request protocol.PermissionGrant) (protocol.PermissionGrant, error) {
	grant := protocol.PermissionGrant{ReadPaths: []string{}, WritePaths: []string{}, Network: request.Network}
	if len(request.ReadPaths)+len(request.WritePaths) > 64 {
		return grant, errors.New("一次权限请求最多包含 64 个路径")
	}
	for _, item := range []struct {
		paths []string
		write bool
		out   *[]string
	}{{request.ReadPaths, false, &grant.ReadPaths}, {request.WritePaths, true, &grant.WritePaths}} {
		seen := map[string]bool{}
		for _, raw := range item.paths {
			path, err := literalPath(raw)
			if err != nil {
				return grant, fmt.Errorf("权限路径必须是已存在的文件或目录：%w", err)
			}
			if filepath.Dir(path) == path {
				return grant, errors.New("不能授权整个磁盘或文件系统")
			}
			for root, access := range p.filesystem {
				if access == "deny" && (within(root, path) || within(path, root)) {
					return grant, errors.New("该路径与执行端的私有数据或身份目录重叠")
				}
				if item.write && access == "read" && root != ":minimal" && (within(root, path) || within(path, root)) {
					return grant, errors.New("不能将只读附件或运行资源改为可写")
				}
			}
			if !seen[path] {
				*item.out = append(*item.out, path)
				seen[path] = true
			}
		}
		sort.Strings(*item.out)
	}
	if len(grant.ReadPaths)+len(grant.WritePaths) == 0 && !grant.Network {
		return grant, errors.New("请求没有包含额外权限")
	}
	return grant, nil
}
