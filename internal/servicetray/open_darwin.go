//go:build darwin

package servicetray

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func OpenURL(address string) error {
	return openHelper(map[string]string{"url": address})
}

func ShowError(title, message string) {
	_ = openHelper(map[string]string{"title": title, "message": message})
}

func openHelper(value map[string]string) error {
	program, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(program, "open-url-helper")
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	command.Stdin = strings.NewReader(string(data))
	return command.Run()
}

// The helper keeps URLs and local panel keys off process command lines.
func RunOpenHelper() error {
	var value struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if json.NewDecoder(os.Stdin).Decode(&value) != nil || value.Message == "" && !strings.HasPrefix(value.URL, "http") {
		return errors.New("a browser address is required on the private input pipe")
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return err
	}
	selector := objc.RegisterName
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selector("new"))
	defer pool.Send(selector("drain"))
	stringValue := func(text string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(selector("stringWithUTF8String:"), text)
	}
	if value.Message != "" {
		app := objc.ID(objc.GetClass("NSApplication")).Send(selector("sharedApplication"))
		app.Send(selector("setActivationPolicy:"), 1)
		app.Send(selector("finishLaunching"))
		app.Send(selector("activateIgnoringOtherApps:"), true)
		alert := objc.ID(objc.GetClass("NSAlert")).Send(selector("new"))
		defer alert.Send(selector("release"))
		alert.Send(selector("setMessageText:"), stringValue(value.Title))
		alert.Send(selector("setInformativeText:"), stringValue(value.Message))
		alert.Send(selector("addButtonWithTitle:"), stringValue("确定"))
		alert.Send(selector("runModal"))
		return nil
	}
	url := objc.ID(objc.GetClass("NSURL")).Send(selector("URLWithString:"), stringValue(value.URL))
	workspace := objc.ID(objc.GetClass("NSWorkspace")).Send(selector("sharedWorkspace"))
	if !objc.Send[bool](workspace, selector("openURL:"), url) {
		return errors.New("cannot open the browser")
	}
	return nil
}
