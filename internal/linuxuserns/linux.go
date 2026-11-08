//go:build linux

package linuxuserns

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/lifecycle"
)

func restricted() bool {
	raw, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
	return err == nil && strings.TrimSpace(string(raw)) == "1"
}

func Guidance() string {
	if restricted() {
		return "AppArmor restricts unprivileged user namespaces; inspect the packaged executor's sandbox-setup command for administrator preparation"
	}
	return "verify that this host permits the user and network namespaces required by Bubblewrap"
}

func trusted(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok || identity.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return fmt.Errorf("preparation path must be root-owned and protected from replacement %s", path)
	}
	if directory {
		if !info.IsDir() || info.Mode().Perm()&0001 == 0 {
			return fmt.Errorf("preparation directory must allow traversal by executor accounts %s", path)
		}
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("preparation file must be a regular file %s", path)
	}
	return nil
}

func trustedAncestors(path string) error {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		if err := trusted(current, true); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if current == "/" {
			return nil
		}
	}
}

func pinnedBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Bubblewrap source must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	if len(raw) > 2<<20 || hex.EncodeToString(digest[:]) != BubblewrapSHA256 {
		return nil, errors.New("Bubblewrap source does not match this executor's pinned digest")
	}
	return raw, nil
}

func installedHelper() (bool, error) {
	if _, err := os.Lstat(HelperPath()); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := trustedAncestors(HelperPath()); err != nil {
		return false, err
	}
	if err := trusted(HelperPath(), false); err != nil {
		return false, err
	}
	info, _ := os.Stat(HelperPath())
	if info == nil || info.Mode().Perm()&0001 == 0 {
		return false, errors.New("prepared Bubblewrap helper is not executable")
	}
	_, err := pinnedBytes(HelperPath())
	return err == nil, err
}

// Environment changes only the native sandbox child. Codex already prefers a
// compatible system Bubblewrap over its bundled launcher.
func Environment(environment []string) ([]string, error) {
	installed, err := installedHelper()
	if err != nil || !installed {
		return environment, err
	}
	result := make([]string, 0, len(environment)+1)
	search := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			search = value
		} else {
			result = append(result, entry)
		}
	}
	return append(result, "PATH="+filepath.Dir(HelperPath())+":"+search), nil
}

func profileLoaded() (bool, error) {
	raw, err := os.ReadFile("/sys/kernel/security/apparmor/profiles")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		name, _, _ := strings.Cut(line, " (")
		if name == profileName() {
			return true, nil
		}
	}
	return false, nil
}

func Inspect(source string) (Plan, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{SourceBwrap: source, SHA256: BubblewrapSHA256, HelperPath: HelperPath(), ProfilePath: profilePath(), ProfileText: profileText(), RestrictionEnabled: restricted(), Administrator: os.Geteuid() == 0}
	if _, err = pinnedBytes(source); err != nil {
		return plan, err
	}
	if plan.HelperInstalled, err = installedHelper(); err != nil {
		return plan, err
	}
	if _, err = os.Lstat(profilePath()); err == nil {
		if err = trustedAncestors(profilePath()); err != nil {
			return plan, err
		}
		if err = trusted(profilePath(), false); err != nil {
			return plan, err
		}
		raw, readErr := os.ReadFile(profilePath())
		if readErr != nil || string(raw) != plan.ProfileText {
			return plan, errors.New("existing AppArmor preparation profile has different contents; inspect it before proceeding")
		}
		plan.ProfileInstalled = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	plan.ProfileLoaded, err = profileLoaded()
	if err != nil && plan.Administrator {
		return plan, err
	}
	plan.Configured = plan.HelperInstalled && plan.ProfileInstalled && plan.ProfileLoaded
	return plan, nil
}

func parser(ctx context.Context, arguments ...string) error {
	command := exec.CommandContext(ctx, parserPath, arguments...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	raw, err := childprocess.OutputContext(ctx, command, 64<<10)
	if err != nil {
		return fmt.Errorf("AppArmor preparation command failed %w %s", err, strings.TrimSpace(string(raw)))
	}
	return nil
}

func createDirectories(path string) error {
	if err := trustedAncestors(filepath.Join(path, "placeholder")); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return trusted(path, true)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := createDirectories(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return trusted(path, true)
		}
		return err
	}
	if err := os.Chmod(path, 0755); err != nil {
		return err
	}
	return trusted(path, true)
}

func installFile(path string, raw []byte, mode os.FileMode) (bool, error) {
	if current, err := os.ReadFile(path); err == nil {
		if err = trusted(path, false); err != nil {
			return false, err
		}
		if !bytes.Equal(current, raw) {
			return false, errors.New("existing preparation file differs from the reviewed content")
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mewdex-preparation-")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err != nil {
		file.Close()
		return false, err
	}
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return false, err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return false, err
	}
	if err = file.Close(); err != nil {
		return false, err
	}
	// Linking refuses an existing destination, including a concurrent writer.
	if err = os.Link(file.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

func Apply(ctx context.Context, source, expectedSHA string, confirmed bool) (Plan, error) {
	if !confirmed || expectedSHA != BubblewrapSHA256 {
		return Plan{}, errors.New("review sandbox-setup inspect, then provide its expected SHA256 and explicit user-namespace confirmation")
	}
	if os.Geteuid() != 0 {
		return Plan{}, errors.New("administrator execution is required to load this AppArmor profile")
	}
	plan, err := Inspect(source)
	if err != nil || plan.Configured {
		return plan, err
	}
	if !plan.RestrictionEnabled {
		return plan, errors.New("this host does not report the AppArmor user-namespace restriction covered by this preparation")
	}
	if plan.ProfileLoaded && !plan.ProfileInstalled {
		return plan, errors.New("a same-named AppArmor profile is already loaded from another source")
	}
	if err = trustedAncestors(parserPath); err != nil {
		return plan, err
	}
	if err = trusted(parserPath, false); err != nil {
		return plan, err
	}
	if err = trusted(profileRoot, true); err != nil {
		return plan, err
	}
	if err = createDirectories(helperRoot); err != nil {
		return plan, err
	}
	lease, err := lifecycle.Acquire(helperRoot)
	if err != nil {
		return plan, err
	}
	defer lease.Close()
	// Re-read after acquiring the machine-local preparation lease.
	plan, err = Inspect(source)
	if err != nil || plan.Configured {
		return plan, err
	}
	raw, err := pinnedBytes(source)
	if err != nil {
		return plan, err
	}
	stage, err := os.CreateTemp(profileRoot, ".mewdex-profile-")
	if err != nil {
		return plan, err
	}
	defer os.Remove(stage.Name())
	if _, err = stage.WriteString(plan.ProfileText); err != nil {
		stage.Close()
		return plan, err
	}
	if err = stage.Close(); err != nil {
		return plan, err
	}
	if err = parser(ctx, "-Q", "-K", stage.Name()); err != nil {
		return plan, err
	}
	if err = createDirectories(filepath.Dir(HelperPath())); err != nil {
		return plan, err
	}
	helperCreated, err := installFile(HelperPath(), raw, 0555)
	if err != nil {
		return plan, err
	}
	profileCreated, err := installFile(profilePath(), []byte(plan.ProfileText), 0644)
	if err == nil {
		err = parser(ctx, "-r", "-K", profilePath())
	}
	if err != nil {
		// Retain pre-existing files. Newly created files have fixed, root-only
		// parents and belong to this still-held preparation lease.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if profileCreated {
			loaded, loadedErr := profileLoaded()
			if loadedErr != nil {
				return plan, errors.Join(err, loadedErr)
			}
			if loaded {
				if removeErr := parser(cleanupCtx, "-R", "-K", profilePath()); removeErr != nil {
					return plan, errors.Join(err, removeErr)
				}
			}
			err = errors.Join(err, os.Remove(profilePath()))
		}
		if helperCreated {
			err = errors.Join(err, os.Remove(HelperPath()))
		}
		return plan, err
	}
	plan, err = Inspect(source)
	if err == nil && !plan.Configured {
		err = errors.New("preparation files exist but AppArmor loading is still unconfirmed")
	}
	return plan, err
}
