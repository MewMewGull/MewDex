//go:build darwin

package startup

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type nativeBackend struct{ directory, domain string }

func Native() (Backend, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &nativeBackend{directory: filepath.Join(home, "Library", "LaunchAgents"), domain: "gui/" + strconv.Itoa(os.Getuid())}, nil
}
func (*nativeBackend) Scope() string { return "user-login" }
func (*nativeBackend) render(d Definition) []byte {
	escape := func(value string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(value))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(xml.Header + "<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n")
	fmt.Fprintf(&b, "<key>Label</key><string>%s</string>\n<key>ProgramArguments</key><array>\n", escape(d.Name))
	for _, argument := range append([]string{d.Program}, d.Arguments...) {
		fmt.Fprintf(&b, "<string>%s</string>\n", escape(argument))
	}
	fmt.Fprintf(&b, "</array>\n<key>WorkingDirectory</key><string>%s</string>\n<key>RunAtLoad</key><true/>\n<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n<key>ThrottleInterval</key><integer>10</integer>\n<key>ProcessType</key><string>Background</string>\n<key>StandardOutPath</key><string>%s</string>\n<key>StandardErrorPath</key><string>%s</string>\n</dict></plist>\n", escape(d.Root), escape(filepath.Join(d.Root, "service.log")), escape(filepath.Join(d.Root, "service.log")))
	return []byte(b.String())
}

func plistArguments(raw []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth := 0
	wanted := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 3 && value.Name.Local == "key" {
				var key string
				if err = decoder.DecodeElement(&key, &value); err != nil {
					return nil, err
				}
				depth--
				wanted = key == "ProgramArguments"
			} else if wanted && depth == 3 && value.Name.Local == "array" {
				var array struct {
					Values []string `xml:"string"`
				}
				if err = decoder.DecodeElement(&array, &value); err != nil {
					return nil, err
				}
				return array.Values, nil
			}
		case xml.EndElement:
			depth--
		}
	}
	return nil, errors.New("launch agent does not contain its program arguments")
}

func (b *nativeBackend) path(d Definition) string   { return filepath.Join(b.directory, d.Name+".plist") }
func (b *nativeBackend) target(d Definition) string { return b.domain + "/" + d.Name }
func launchOwner(raw []byte, d Definition) string {
	arguments, err := plistArguments(raw)
	if err != nil || len(arguments) != 6 || arguments[1] != "managed" || arguments[2] != "-root" || arguments[3] != d.Root || arguments[4] != "-owner" {
		return ""
	}
	return arguments[5]
}
func (b *nativeBackend) Inspect(ctx context.Context, d Definition) (Entry, error) {
	raw, err := readEntry(b.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, err
	}
	status, err := run(ctx, "/bin/launchctl", []string{"print-disabled", b.domain}, nil)
	if err != nil {
		return Entry{}, errors.New("the macOS user login session is unavailable")
	}
	disabled := regexp.MustCompile(`(?m)^\s*"` + regexp.QuoteMeta(d.Name) + `"\s*=>\s*true\s*$`).Match(status)
	active, _ := run(ctx, "/bin/launchctl", []string{"print", b.target(d)}, nil)
	return Entry{Present: true, Owner: launchOwner(raw, d), Enabled: !disabled, Running: regexp.MustCompile(`(?m)^\s*state = running\s*$`).Match(active), Matches: bytes.Equal(raw, b.render(d)), Raw: raw}, nil
}
func (b *nativeBackend) owned(d Definition) ([]byte, error) {
	raw, err := readEntry(b.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if launchOwner(raw, d) != d.Owner {
		return nil, errors.New("launch agent belongs to another installation")
	}
	return raw, nil
}
func (b *nativeBackend) Install(ctx context.Context, d Definition) error {
	before, err := b.owned(d)
	if err != nil {
		return err
	}
	if err = writeEntry(b.path(d), before, b.render(d)); err != nil {
		return err
	}
	_, err = run(ctx, "/bin/launchctl", []string{"enable", b.target(d)}, nil)
	return err // Register for the next login; do not bootstrap or start it now.
}
func (b *nativeBackend) Remove(ctx context.Context, d Definition) error {
	before, err := b.owned(d)
	if err != nil || before == nil {
		return err
	}
	if _, err = run(ctx, "/bin/launchctl", []string{"disable", b.target(d)}, nil); err != nil {
		return err
	}
	return removeEntry(b.path(d), before) // A currently running service is not stopped.
}
func (b *nativeBackend) Start(ctx context.Context, d Definition) error {
	entry, err := b.Inspect(ctx, d)
	if err != nil {
		return err
	}
	if entry.Owner != d.Owner {
		return errors.New("launch agent belongs to another installation")
	}
	if entry.Running {
		return nil
	}
	if _, err = run(ctx, "/bin/launchctl", []string{"print", b.target(d)}, nil); err == nil {
		if _, err = run(ctx, "/bin/launchctl", []string{"bootout", b.target(d)}, nil); err != nil {
			return err
		}
	}
	_, err = run(ctx, "/bin/launchctl", []string{"bootstrap", b.domain, b.path(d)}, nil)
	return err
}
func (b *nativeBackend) Stop(ctx context.Context, d Definition) error {
	if _, err := b.owned(d); err != nil {
		return err
	}
	if _, err := run(ctx, "/bin/launchctl", []string{"print", b.target(d)}, nil); err != nil {
		return nil
	}
	_, err := run(ctx, "/bin/launchctl", []string{"bootout", b.target(d)}, nil)
	return err
}
