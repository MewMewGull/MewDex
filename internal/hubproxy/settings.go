// Package hubproxy owns the optional SSH reverse tunnel inside the Hub process.
package hubproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
)

const settingsName = "hub-proxy.json"

type Settings struct {
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	IdentityFile   string `json:"identityFile"`
	KnownHostsFile string `json:"knownHostsFile"`
	RemoteBind     string `json:"remoteBind"`
	RemotePort     int    `json:"remotePort"`
	PublicURL      string `json:"publicUrl"`
}

func Defaults() Settings { return Settings{Port: 22, RemoteBind: "127.0.0.1", RemotePort: 47834} }

func readJSON(path string, value any) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<10 {
		return nil, errors.New("代理设置必须是小于 32 KB 的普通文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (32<<10)+1))
	if err != nil || len(raw) > 32<<10 {
		return nil, errors.New("无法读取代理设置")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("代理设置只能包含一个 JSON 对象")
	}
	return raw, nil
}

func Read(root string) (Settings, string, error) {
	s := Defaults()
	raw, err := readJSON(filepath.Join(root, settingsName), &s)
	if errors.Is(err, os.ErrNotExist) {
		return s, "", nil
	}
	return s, startup.Fingerprint(raw), err
}

func Save(root string, s Settings) error {
	// Disabling must still work if a key or server is no longer available.
	if s.Enabled {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return atomicJSON(root, settingsName, s)
}

func atomicJSON(dir, name string, value any) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := name + "." + protocol.ID() + ".tmp"
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	_, writeErr := f.Write(protocol.JSON(value))
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return r.Rename(tmp, name)
}

func (s Settings) Validate() error {
	if s.Host == "" || strings.ContainsAny(s.Host, "\r\n\x00 /\\@") || s.User == "" || strings.ContainsAny(s.User, "\r\n\x00") {
		return errors.New("请填写 SSH 主机和用户")
	}
	for _, port := range []int{s.Port, s.RemotePort} {
		if port < 1 || port > 65535 {
			return errors.New("端口必须在 1–65535 之间")
		}
	}
	ip := net.ParseIP(s.RemoteBind)
	if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return errors.New("远端监听地址必须是明确的回环或私网 IP")
	}
	u, err := url.Parse(s.PublicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("公网 Hub 地址需要是无路径、凭据、查询参数的 HTTPS 地址")
	}
	_, err = s.authentication()
	return err
}

func (s Settings) authentication() (*ssh.ClientConfig, error) {
	for _, path := range []string{s.IdentityFile, s.KnownHostsFile} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return nil, errors.New("请选择私钥和 known_hosts 文件的绝对路径")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return nil, errors.New("SSH 私钥或 known_hosts 文件不可用")
		}
	}
	f, err := os.Open(s.IdentityFile)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	_ = f.Close()
	if readErr != nil || len(raw) > 1<<20 {
		return nil, errors.New("无法读取 SSH 私钥")
	}
	signer, err := ssh.ParsePrivateKey(raw)
	clear(raw)
	if err != nil {
		return nil, errors.New("无法使用此 SSH 私钥；静默连接需要无需交互解锁的专用私钥")
	}
	verify, err := knownhosts.New(s.KnownHostsFile)
	if err != nil {
		return nil, errors.New("无法读取 SSH known_hosts；请先核对服务器主机密钥")
	}
	return &ssh.ClientConfig{User: s.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: verify}, nil
}

func LocalTarget(listen string) (string, error) {
	_, port, err := net.SplitHostPort(listen)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 1 || n > 65535 {
		return "", errors.New("请先为 Hub 设置固定的监听端口")
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}
