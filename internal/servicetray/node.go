package servicetray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"mewdex/internal/config"
	"mewdex/internal/hubproxy"
	"mewdex/internal/lifecycle"
	"mewdex/internal/localnode"
	"mewdex/internal/startup"
)

func Node(root string, stop context.CancelFunc) (Options, error) {
	program, err := os.Executable()
	if err != nil {
		return Options{}, err
	}
	configuration, err := config.Read(filepath.Join(root, "device.json"))
	if err != nil {
		return Options{}, err
	}
	manager, err := localnode.New(localnode.Options{Root: root, Program: program})
	if err != nil {
		return Options{}, err
	}
	role := "执行端"
	iconRole := ExecutorRole
	if configuration.Hub {
		role = "Hub"
		iconRole = HubRole
	}
	return Options{
		Role:  iconRole,
		Title: "MewDex · " + role,
		Menu: func() []Item {
			var record startup.Record
			raw, _ := os.ReadFile(filepath.Join(root, "startup.json"))
			_ = json.Unmarshal(raw, &record)
			startupItem := Item{ID: "startup.enable", Label: "开机自启动", Question: "启用此组件的开机自启动？"}
			if record.Enabled {
				startupItem.ID, startupItem.Question, startupItem.Checked = "startup.disable", "关闭此组件的开机自启动？", true
			}
			items := []Item{{ID: "open", Label: "打开控制面板"}, {ID: "status", Label: "查看状态"}, {Separator: true}, startupItem}
			if configuration.Hub {
				settings, _, _ := hubproxy.Read(root)
				items = append(items, Item{ID: "proxy.toggle", Label: "公网反向代理", Checked: settings.Enabled})
			}
			return append(items, Item{Separator: true}, Item{ID: "quit", Label: "退出 " + role, Question: "退出将停止此组件及其进行中的任务。是否继续？"})
		},
		Action: func(ctx context.Context, id string) (string, error) {
			result, err := manager.Handle(ctx, localnode.Request{Operation: "status"})
			if err != nil {
				return "", err
			}
			if result.Snapshot.DeviceID != configuration.DeviceID {
				return "", errors.New("设备身份已变化，请刷新组件状态")
			}
			switch id {
			case "open":
				command := exec.Command(program, "panel", "-root", root, "-open")
				detach(command)
				if err = command.Start(); err != nil {
					return "", err
				}
				go command.Wait()
				return "", nil
			case "status":
				text := fmt.Sprintf("组件：%s\n设备：%s\n服务：%s", role, result.Snapshot.Name, result.Snapshot.State)
				if connection := result.Snapshot.HubConnection; connection != nil {
					text += "\nHub 连接：" + connection.State
				}
				return text, nil
			case "startup.enable", "startup.disable":
				_, err = manager.Handle(ctx, localnode.Request{Operation: "startup.apply", ExpectedDeviceID: configuration.DeviceID, Startup: &localnode.StartupRequest{Enabled: id == "startup.enable"}})
				return "", err
			case "proxy.toggle":
				settings, _, readErr := hubproxy.Read(root)
				if readErr != nil {
					return "", readErr
				}
				settings.Enabled = !settings.Enabled
				_, err = manager.Handle(ctx, localnode.Request{Operation: "proxy.apply", ExpectedDeviceID: configuration.DeviceID, HubProxy: (*localnode.HubProxySettings)(&settings)})
				return "", err
			case "quit":
				lease, lockErr := lifecycle.Acquire(filepath.Join(root, "management"))
				if lockErr != nil {
					return "", lockErr
				}
				defer lease.Close()
				backend, backendErr := startup.Native()
				if backendErr != nil {
					return "", backendErr
				}
				controller, controllerErr := startup.New(root, backend)
				if controllerErr != nil {
					return "", controllerErr
				}
				if err = controller.PauseRecovery(); err != nil {
					return "", err
				}
				stop()
				return "", nil
			default:
				return "", errors.New("unknown service tray action")
			}
		},
	}, nil
}
