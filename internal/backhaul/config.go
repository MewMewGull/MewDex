// Package backhaul maintains an operator-configured SSH return path for an
// existing self-hosted HTTPS ingress. It is not a device mesh or NAT service.
package backhaul

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mewdex/internal/startup"
)

type Config struct {
	ID             string `json:"id"`
	SSHPath        string `json:"sshPath"`
	SSHSHA256      string `json:"sshSha256"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	IdentityFile   string `json:"identityFile"`
	KnownHostsFile string `json:"knownHostsFile"`
	RemoteBind     string `json:"remoteBind"`
	RemotePort     int    `json:"remotePort"`
	LocalPort      int    `json:"localPort"`
	PublicURL      string `json:"publicUrl"`
}

var atom = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$`)

func Read(root string) (Config, startup.Binding, error) {
	var c Config
	profile := filepath.Join(root, "backhaul.json")
	info, err := os.Lstat(profile)
	if err != nil {
		return c, startup.Binding{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<10 {
		return c, startup.Binding{}, errors.New("backhaul profile must be a bounded regular file")
	}
	file, err := os.Open(profile)
	if err != nil {
		return c, startup.Binding{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (32<<10)+1))
	if err != nil || len(raw) > 32<<10 {
		return c, startup.Binding{}, errors.New("backhaul profile exceeds its read limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, startup.Binding{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return c, startup.Binding{}, errors.New("backhaul profile must contain exactly one object")
	}
	if err := c.validate(); err != nil {
		return c, startup.Binding{}, err
	}
	return c, startup.Binding{DeviceID: c.ID, ProfileSHA256: startup.Fingerprint(raw)}, nil
}

func (c Config) validate() error {
	for _, value := range []string{c.ID, c.User} {
		if !atom.MatchString(value) {
			return errors.New("backhaul identity and SSH user must be explicit single names")
		}
	}
	if !atom.MatchString(c.Host) && net.ParseIP(c.Host) == nil {
		return errors.New("backhaul SSH host must be a literal hostname or IP")
	}
	for _, port := range []int{c.Port, c.RemotePort, c.LocalPort} {
		if port < 1 || port > 65535 {
			return errors.New("backhaul ports must be between 1 and 65535")
		}
	}
	ip := net.ParseIP(c.RemoteBind)
	if ip == nil || !ip.IsLoopback() && !ip.IsPrivate() {
		return errors.New("backhaul listener must use an explicit private or loopback address")
	}
	for _, path := range []string{c.SSHPath, c.IdentityFile, c.KnownHostsFile} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("SSH program and credential references must be absolute paths")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("an existing SSH program or credential reference is unavailable")
		}
	}
	if len(c.SSHSHA256) != 64 || strings.Trim(c.SSHSHA256, "0123456789abcdef") != "" {
		return errors.New("backhaul requires the reviewed SSH executable hash")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("backhaul public URL must not include credentials, query or fragment")
	}
	loopback := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback != nil && loopback.IsLoopback()) {
		return errors.New("backhaul public ingress requires HTTPS; HTTP is limited to loopback development")
	}
	return nil
}

func (c Config) VerifySSH() error {
	file, err := os.Open(c.SSHPath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, (64<<20)+1))
	if err != nil || count > 64<<20 || hex.EncodeToString(hash.Sum(nil)) != c.SSHSHA256 {
		return errors.New("SSH executable changed; review its hash before starting the backhaul")
	}
	return nil
}

func (c Config) localURL() string { return "http://127.0.0.1:" + strconv.Itoa(c.LocalPort) }

func (c Config) sshArguments() []string {
	// No shell, inherited SSH configuration, daemonization, remote command,
	// agent/X11 forwarding, local command, multiplexed connection or prompt.
	return []string{"-F", "none", "-N", "-T", "-S", "none",
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ExitOnForwardFailure=yes",
		"-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "PermitLocalCommand=no", "-o", "ControlMaster=no",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=10", "-o", "ConnectionAttempts=1", "-o", "LogLevel=ERROR",
		"-o", "UserKnownHostsFile=" + strconv.Quote(strings.ReplaceAll(filepath.ToSlash(c.KnownHostsFile), "%", "%%")), "-o", "GlobalKnownHostsFile=none",
		"-i", strings.ReplaceAll(c.IdentityFile, "%", "%%"), "-l", c.User, "-p", strconv.Itoa(c.Port),
		"-R", net.JoinHostPort(c.RemoteBind, strconv.Itoa(c.RemotePort)) + ":127.0.0.1:" + strconv.Itoa(c.LocalPort), c.Host}
}
