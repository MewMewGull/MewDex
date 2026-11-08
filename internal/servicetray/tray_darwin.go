//go:build darwin

package servicetray

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"structs"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func init() { runtime.LockOSThread() }

func Start(parent context.Context, options Options) (func(), error) {
	if options.Title == "" || options.Menu == nil || options.Action == nil {
		return nil, errors.New("service tray requires its title, menu and action handler")
	}
	program, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(program, "tray-helper")
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = command.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	var write sync.Mutex
	send := func(value view) error {
		write.Lock()
		defer write.Unlock()
		return json.NewEncoder(input).Encode(value)
	}
	ready := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer cancel()
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			var value event
			if json.Unmarshal(scanner.Bytes(), &value) != nil {
				continue
			}
			if value.Type == "ready" {
				if value.Visible {
					ready <- nil
					fmt.Fprintf(os.Stderr, "MewDex menu bar ready title=%q helperPid=%d\n", options.Title, command.Process.Pid)
				} else {
					ready <- errors.New("the macOS status item is unavailable")
				}
				continue
			}
			if value.Type == "action" {
				operation, stop := context.WithTimeout(ctx, 30*time.Second)
				text, actionErr := options.Action(operation, value.ID)
				stop()
				if actionErr != nil {
					text = actionErr.Error()
				}
				if ctx.Err() == nil {
					_ = send(view{Role: options.Role, Title: options.Title, Items: options.Menu(), Message: text, Error: actionErr != nil})
				}
			}
		}
		_ = command.Wait()
	}()
	closePipe := context.AfterFunc(ctx, func() { _ = input.Close() })
	var once sync.Once
	closeTray := func() {
		once.Do(func() {
			cancel()
			_ = input.Close()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				<-exited
			}
			closePipe()
		})
	}
	if err = send(view{Role: options.Role, Title: options.Title, Items: options.Menu()}); err != nil {
		closeTray()
		return nil, err
	}
	select {
	case err = <-ready:
	case <-ctx.Done():
		err = errors.New("the macOS menu bar helper exited before becoming ready")
	case <-time.After(15 * time.Second):
		err = errors.New("the macOS menu bar helper did not become ready")
	}
	if err != nil {
		closeTray()
		return nil, err
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = send(view{Role: options.Role, Title: options.Title, Items: options.Menu()})
			}
		}
	}()
	return closeTray, nil
}

type nativeSize struct {
	_             structs.HostLayout
	Width, Height float64
}

func RunHelper() error {
	decoder := json.NewDecoder(os.Stdin)
	var initial view
	if err := decoder.Decode(&initial); err != nil || initial.Title == "" {
		return errors.New("service tray configuration is required on its private input pipe")
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return err
	}
	selector := objc.RegisterName
	stringValue := func(value string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(selector("stringWithUTF8String:"), value)
	}
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selector("new"))
	defer pool.Send(selector("drain"))
	application := objc.ID(objc.GetClass("NSApplication")).Send(selector("sharedApplication"))
	application.Send(selector("setActivationPolicy:"), 1)
	status := objc.ID(objc.GetClass("NSStatusBar")).Send(selector("systemStatusBar")).Send(selector("statusItemWithLength:"), float64(-1))
	if status == 0 {
		return errors.New("cannot create a macOS menu bar status item")
	}
	status.Send(selector("retain"))
	button := status.Send(selector("button"))
	statusImage := iconBytes(initial.Role, "png")
	data := objc.ID(objc.GetClass("NSData")).Send(selector("dataWithBytes:length:"), unsafe.Pointer(&statusImage[0]), uintptr(len(statusImage)))
	image := objc.ID(objc.GetClass("NSImage")).Send(selector("alloc")).Send(selector("initWithData:"), data)
	image.Send(selector("setSize:"), nativeSize{Width: 18, Height: 18})
	button.Send(selector("setImage:"), image)
	button.Send(selector("setToolTip:"), stringValue(initial.Title))
	var pending sync.Mutex
	var updates []view
	current := initial
	var delegate objc.ID
	var render func(view)
	show := func(text string, question bool) bool {
		application.Send(selector("activateIgnoringOtherApps:"), true)
		alert := objc.ID(objc.GetClass("NSAlert")).Send(selector("new"))
		defer alert.Send(selector("release"))
		alert.Send(selector("setMessageText:"), stringValue(initial.Title))
		alert.Send(selector("setInformativeText:"), stringValue(text))
		if question {
			alert.Send(selector("addButtonWithTitle:"), stringValue("取消"))
			alert.Send(selector("addButtonWithTitle:"), stringValue("继续"))
			return int(alert.Send(selector("runModal"))) == 1001
		}
		alert.Send(selector("addButtonWithTitle:"), stringValue("确定"))
		alert.Send(selector("runModal"))
		return true
	}
	class, err := objc.RegisterClass("MewDexServiceTrayDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: selector("performAction:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) {
			index := int(sender.Send(selector("tag")))
			if index < 0 || index >= len(current.Items) {
				return
			}
			item := current.Items[index]
			if !item.Separator && (item.Question == "" || show(item.Question, true)) {
				_ = json.NewEncoder(os.Stdout).Encode(event{Type: "action", ID: item.ID})
			}
		}},
		{Cmd: selector("applyUpdates:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) {
			pending.Lock()
			values := updates
			updates = nil
			pending.Unlock()
			for _, value := range values {
				if !reflect.DeepEqual(current.Items, value.Items) {
					render(value)
				}
				if value.Message != "" {
					show(value.Message, false)
				}
			}
		}},
		{Cmd: selector("finish:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) {
			application.Send(selector("terminate:"), objc.ID(0))
		}},
	})
	if err != nil {
		return err
	}
	delegate = objc.ID(class).Send(selector("new"))
	render = func(value view) {
		menu := objc.ID(objc.GetClass("NSMenu")).Send(selector("new"))
		defer menu.Send(selector("release"))
		menu.Send(selector("setAutoenablesItems:"), false)
		for index, item := range value.Items {
			var entry objc.ID
			if item.Separator {
				entry = objc.ID(objc.GetClass("NSMenuItem")).Send(selector("separatorItem"))
			} else {
				entry = objc.ID(objc.GetClass("NSMenuItem")).Send(selector("alloc")).Send(selector("initWithTitle:action:keyEquivalent:"), stringValue(item.Label), selector("performAction:"), stringValue(""))
				entry.Send(selector("setTarget:"), delegate)
				entry.Send(selector("setTag:"), index)
				if item.Checked {
					entry.Send(selector("setState:"), 1)
				}
			}
			menu.Send(selector("addItem:"), entry)
			if !item.Separator {
				entry.Send(selector("release"))
			}
		}
		status.Send(selector("setMenu:"), menu)
		current = value
	}
	render(initial)
	application.Send(selector("finishLaunching"))
	if err = json.NewEncoder(os.Stdout).Encode(event{Type: "ready", Visible: objc.Send[bool](status, selector("isVisible"))}); err != nil {
		return err
	}
	go func() {
		for {
			var value view
			if decoder.Decode(&value) != nil {
				delegate.Send(selector("performSelectorOnMainThread:withObject:waitUntilDone:"), selector("finish:"), objc.ID(0), false)
				return
			}
			pending.Lock()
			updates = append(updates, value)
			pending.Unlock()
			delegate.Send(selector("performSelectorOnMainThread:withObject:waitUntilDone:"), selector("applyUpdates:"), objc.ID(0), false)
		}
	}()
	application.Send(selector("run"))
	runtime.KeepAlive(statusImage)
	runtime.KeepAlive(delegate)
	return nil
}
