package hubproxy

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	sshconfig "github.com/kevinburke/ssh_config"
)

// SSHConfigInfo contains connection metadata only. Importing never reads a
// private key, runs OpenSSH commands, writes settings, or opens a connection.
type SSHConfigInfo struct {
	ConfigPath string       `json:"configPath"`
	Found      bool         `json:"found"`
	Profiles   []SSHProfile `json:"profiles"`
	Message    string       `json:"message"`
}

type SSHProfile struct {
	Alias          string   `json:"alias"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	User           string   `json:"user"`
	IdentityFile   string   `json:"identityFile"`
	KnownHostsFile string   `json:"knownHostsFile"`
	Importable     bool     `json:"importable"`
	Warnings       []string `json:"warnings"`
}

// ReadSSHConfig reads just the current user's static config. Conditional
// includes and Match directives are left to the manual form: evaluating them
// through ssh -G can execute Match exec, and ignoring them can select a wrong
// endpoint. ProxyCommand/ProxyJump are likewise never executed.
func ReadSSHConfig(home string) (SSHConfigInfo, error) {
	info := SSHConfigInfo{ConfigPath: filepath.Join(home, ".ssh", "config"), Profiles: []SSHProfile{}}
	if !filepath.IsAbs(home) {
		return info, errors.New("无法确定本机用户的 SSH 配置目录")
	}
	file, err := os.Open(info.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		info.Message = "未找到本机 .ssh/config，可继续使用下方表单手动配置。"
		return info, nil
	}
	if err != nil {
		return info, errors.New("无法读取本机 .ssh/config，请检查文件访问权限")
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 1<<20 {
		return info, errors.New("SSH 配置必须是小于 1 MB 的普通文件")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return info, errors.New("无法读取有效的 SSH 配置")
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	// Inspect directive names without parsing or executing their values.
	for _, line := range strings.Split(string(raw), "\n") {
		if !balancedSSHQuotes(line) {
			return info, errors.New("SSH 配置存在未闭合的引号，请检查 .ssh/config 或使用手动表单")
		}
		fields := strings.FieldsFunc(strings.TrimSpace(line), func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
		if len(fields) > 0 && (strings.EqualFold(fields[0], "Include") || strings.EqualFold(fields[0], "Match")) {
			return info, errors.New("此 SSH 配置包含 Include 或 Match 条件，本次不自动解析；请使用下方表单手动填写")
		}
	}
	config, err := decodeSSHConfig(raw)
	if err != nil {
		// Parser diagnostics can quote arbitrary directive values. Do not return
		// raw config lines, commands or potential embedded credentials to the UI.
		return info, errors.New("SSH 配置无法解析，请检查 .ssh/config 格式或使用手动表单")
	}
	info.Found = true
	aliases := []string{}
	for _, host := range config.Hosts {
		for _, pattern := range host.Patterns {
			alias := pattern.String()
			if alias == "" || strings.ContainsAny(alias, "!*?\r\n\x00") || len(alias) > 256 || slices.Contains(aliases, alias) {
				continue
			}
			aliases = append(aliases, alias)
			if len(aliases) > 200 {
				return info, errors.New("SSH 主机条目超过 200 个，请使用手动表单")
			}
		}
	}
	for _, alias := range aliases {
		info.Profiles = append(info.Profiles, resolveSSHProfile(config, alias, home))
	}
	if len(info.Profiles) == 0 {
		info.Message = "配置中没有可选择的具体 Host 别名；通配符默认项仍可用于具体 Host。"
	} else {
		info.Message = "选择 SSH 主机并预览连接信息，再填入下方表单；原 SSH 文件保持不变。"
	}
	return info, nil
}

func decodeSSHConfig(raw []byte) (config *sshconfig.Config, err error) {
	defer func() {
		if recover() != nil {
			config = nil
			err = errors.New("SSH 配置格式无效")
		}
	}()
	return sshconfig.DecodeBytes(raw)
}

func balancedSSHQuotes(line string) bool {
	quoted, escaped := false, false
	for _, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if char == '#' && !quoted {
			break
		}
		if char == '\\' && quoted {
			escaped = true
		} else if char == '"' {
			quoted = !quoted
		}
	}
	return !quoted
}

func resolveSSHProfile(config *sshconfig.Config, alias, home string) SSHProfile {
	profile := SSHProfile{Alias: alias, Host: alias, Port: 22, Importable: true, Warnings: []string{}}
	get := func(key string) string { value, _ := config.Get(alias, key); return strings.TrimSpace(value) }
	if value := get("HostName"); value != "" {
		if host, err := expandSSHValue(value, map[byte]string{'h': alias, 'n': alias}); err == nil {
			profile.Host = host
		} else {
			profile.Importable = false
			profile.Warnings = append(profile.Warnings, "服务器名称包含暂不支持的占位符，请手动填写。")
		}
	}
	if value := get("Port"); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			profile.Importable = false
			profile.Warnings = append(profile.Warnings, "SSH 端口无效，请手动填写。")
		} else {
			profile.Port = port
		}
	}
	localUser := filepath.Base(home)
	if current, err := user.Current(); err == nil && current.Username != "" {
		localUser = current.Username
		if index := strings.LastIndexAny(localUser, "\\/"); index >= 0 {
			localUser = localUser[index+1:]
		}
	}
	profile.User = get("User")
	if profile.User == "" {
		profile.User = localUser
	}
	if profile.Host == "" || strings.ContainsAny(profile.Host, "\r\n\x00 /\\@") || profile.User == "" || strings.ContainsAny(profile.User, "\r\n\x00") {
		profile.Importable = false
		profile.Warnings = append(profile.Warnings, "服务器或用户名称无效，请手动填写。")
	}
	for _, key := range []string{"ProxyCommand", "ProxyJump", "HostKeyAlias"} {
		if value := get(key); value != "" && !strings.EqualFold(value, "none") {
			profile.Importable = false
			profile.Warnings = append(profile.Warnings, "此条目使用 "+key+"；当前反向代理需要可直连的 SSH 主机，请手动配置。")
		}
	}
	tokens := map[byte]string{'d': home, 'h': profile.Host, 'n': alias, 'p': strconv.Itoa(profile.Port), 'r': profile.User, 'u': localUser}
	identities, _ := config.GetAll(alias, "IdentityFile")
	if len(identities) == 0 {
		identities = []string{"~/.ssh/id_ed25519", "~/.ssh/id_ecdsa", "~/.ssh/id_rsa"}
	}
	for _, value := range identities {
		path, err := sshFilePath(value, home, tokens)
		if err != nil {
			continue
		}
		if profile.IdentityFile == "" {
			profile.IdentityFile = path
		}
		if regularSSHFile(path) {
			profile.IdentityFile = path
			break
		}
	}
	if !regularSSHFile(profile.IdentityFile) {
		profile.Importable = false
		profile.Warnings = append(profile.Warnings, "未找到可用的私钥文件，请在手动表单中指定现有文件。")
	}
	hosts := get("UserKnownHostsFile")
	if hosts == "" {
		hosts = "~/.ssh/known_hosts"
	}
	profile.KnownHostsFile, _ = sshFilePath(hosts, home, tokens)
	if !regularSSHFile(profile.KnownHostsFile) {
		profile.Importable = false
		profile.Warnings = append(profile.Warnings, "未找到可用的 known_hosts 文件，请先核对服务器主机密钥并在表单中指定文件。")
	}
	return profile
}

func regularSSHFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() <= 1<<20
}

func sshFilePath(value, home string, tokens map[byte]string) (string, error) {
	value, err := expandSSHValue(strings.TrimSpace(value), tokens)
	if err != nil || strings.ContainsAny(value, "\r\n\x00") || strings.HasSuffix(strings.ToLower(value), ".pub") {
		return "", errors.New("不支持的 SSH 文件路径")
	}
	if value == "~" {
		value = home
	} else if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		value = filepath.Join(home, value[2:])
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("SSH 文件需要绝对路径或 ~/ 路径")
	}
	return filepath.Clean(value), nil
}

func expandSSHValue(value string, tokens map[byte]string) (string, error) {
	if strings.Contains(value, "${") {
		return "", errors.New("不展开环境变量")
	}
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		index++
		if index >= len(value) {
			return "", errors.New("不完整的 SSH 占位符")
		}
		if value[index] == '%' {
			result.WriteByte('%')
		} else if replacement, exists := tokens[value[index]]; exists {
			result.WriteString(replacement)
		} else {
			return "", errors.New("不支持的 SSH 占位符")
		}
	}
	return result.String(), nil
}
