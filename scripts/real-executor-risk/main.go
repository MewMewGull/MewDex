// A bounded real-model development check. It uses an existing local ChatGPT
// account without reading or copying its credentials and never changes a live
// service. A previously created root is observation-only; inputs are not replayed.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"mewdex/internal/attachments"
	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
)

type probe struct {
	Root                     string  `json:"root"`
	At                       string  `json:"at"`
	State                    string  `json:"state"`
	Error                    string  `json:"error,omitempty"`
	EngineVersion            string  `json:"engineVersion"`
	Model                    string  `json:"model"`
	SessionID                string  `json:"sessionId,omitempty"`
	ConfigSHA256             string  `json:"globalConfigSha256"`
	GlobalConfigUnchanged    bool    `json:"globalConfigUnchanged"`
	HooksDisabled            bool    `json:"hooksDisabled"`
	ExternalToolSetsDisabled int     `json:"externalToolSetsDisabled"`
	SignedIn                 bool    `json:"signedIn"`
	AccountType              string  `json:"accountType,omitempty"`
	PlanType                 string  `json:"planType,omitempty"`
	Phases                   []phase `json:"phases"`
	LiveServicesUpdated      bool    `json:"liveServicesUpdated"`
	PackagingValidation      bool    `json:"packagingValidation"`
	ModelCredentialsCopied   bool    `json:"modelCredentialsCopied"`
}

type phase struct {
	Name               string `json:"name"`
	CommandID          string `json:"commandId"`
	State              string `json:"state"`
	EngineID           string `json:"engineId,omitempty"`
	TurnID             string `json:"turnId,omitempty"`
	ScopeID            string `json:"scopeId,omitempty"`
	NativeCommandItems int    `json:"nativeCommandItems"`
	OutputVerified     bool   `json:"outputVerified"`
	HistoryIndexRead   bool   `json:"historyIndexRead,omitempty"`
}

type fixtureResult struct {
	ProbeValue string   `json:"probeValue"`
	Colors     []string `json:"colors"`
	SourcePath string   `json:"sourcePath"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func hashConfig(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func boundedJSON(root, name string, target any) error {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	info, err := directory.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return errors.New("model output must be a bounded regular file")
	}
	file, err := directory.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return err
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected model output contents")
	}
	return nil
}

func effectiveConfig(ctx context.Context, native *engine.Codex) (map[string]json.RawMessage, error) {
	var reply struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	err := native.Call(ctx, "config/read", map[string]bool{"includeLayers": false}, &reply)
	return reply.Config, err
}

func disabledToolSetOverride(group string, names []string) (string, error) {
	if group != "mcp_servers" && group != "plugins" {
		return "", errors.New("unknown tool set group")
	}
	names = append([]string(nil), names...)
	sort.Strings(names)
	values := []string{}
	for i, name := range names {
		if name == "" || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return "", errors.New("invalid configured tool set name")
		}
		if i > 0 && names[i-1] == name {
			continue
		}
		values = append(values, fmt.Sprintf(`%q={enabled=false}`, name))
	}
	// CLI dotted keys do not parse quoted path segments. Quote the name in a
	// TOML table VALUE instead, so dots and plugin identifiers remain one key.
	return group + "={" + strings.Join(values, ",") + "}", nil
}

func nativeForProbe(ctx context.Context, program, workspace string, diagnostics io.Writer) (*engine.Codex, int, error) {
	args := []string{"--strict-config", "-c", `model_provider="openai"`, "-c", `notify=[]`, "-c", `features.hooks=false`, "-c", `features.codex_hooks=false`, "-c", `features.apps=false`, "-c", `features.multi_agent=false`, "-c", `features.goals=false`, "-c", `features.memories=false`, "-c", fmt.Sprintf(`projects={%q={trust_level="trusted"}}`, workspace)}
	native, err := engine.StartWithOptions(ctx, config.Engine{Executable: program, Args: args}, engine.StartOptions{Diagnostics: diagnostics})
	if err != nil {
		return nil, 0, err
	}
	values, err := effectiveConfig(ctx, native)
	if err != nil {
		native.Close()
		return nil, 0, err
	}
	count := 0
	for _, group := range []string{"mcp_servers", "plugins"} {
		var entries map[string]json.RawMessage
		if json.Unmarshal(values[group], &entries) != nil {
			continue
		}
		names := []string{}
		for name, entry := range entries {
			var settings map[string]json.RawMessage
			if json.Unmarshal(entry, &settings) != nil {
				continue
			}
			names = append(names, name)
		}
		if len(names) > 0 {
			override, err := disabledToolSetOverride(group, names)
			if err != nil {
				native.Close()
				return nil, 0, err
			}
			args = append(args, "-c", override)
			count += len(names)
		}
	}
	if count > 0 {
		if err = native.Close(); err != nil {
			return nil, 0, err
		}
		native, err = engine.StartWithOptions(ctx, config.Engine{Executable: program, Args: args}, engine.StartOptions{Diagnostics: diagnostics})
		if err != nil {
			return nil, 0, err
		}
		values, err = effectiveConfig(ctx, native)
		if err != nil {
			native.Close()
			return nil, 0, err
		}
	}
	var notify []string
	var features map[string]json.RawMessage
	_ = json.Unmarshal(values["notify"], &notify)
	_ = json.Unmarshal(values["features"], &features)
	for _, name := range []string{"hooks", "codex_hooks", "apps", "multi_agent", "goals", "memories"} {
		var enabled bool
		if json.Unmarshal(features[name], &enabled) != nil || enabled {
			native.Close()
			return nil, 0, errors.New("probe feature isolation could not be confirmed")
		}
	}
	if len(notify) != 0 {
		native.Close()
		return nil, 0, errors.New("legacy notification commands remain enabled")
	}
	for _, group := range []string{"mcp_servers", "plugins"} {
		var entries map[string]json.RawMessage
		if json.Unmarshal(values[group], &entries) != nil {
			continue
		}
		for _, entry := range entries {
			var settings map[string]json.RawMessage
			if json.Unmarshal(entry, &settings) != nil {
				continue
			}
			var enabled bool
			if json.Unmarshal(settings["enabled"], &enabled) != nil || enabled {
				native.Close()
				return nil, 0, fmt.Errorf("configured external tool set was not disabled (%s, explicit=%t, enabled=%t)", group, len(settings["enabled"]) > 0, enabled)
			}
		}
	}
	return native, count, nil
}

func writeReport(report *probe) error {
	file, err := os.CreateTemp(report.Root, "report-*.tmp")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	_, err = file.Write(protocol.JSON(report))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path, filepath.Join(report.Root, "report.json"))
}

type boundedLog struct {
	file      *os.File
	remaining int
}

func (w *boundedLog) Write(value []byte) (int, error) {
	count := len(value)
	if w.remaining <= 0 {
		return count, nil
	}
	if len(value) > w.remaining {
		value = value[:w.remaining]
	}
	written, err := w.file.Write(value)
	w.remaining -= written
	if err != nil {
		return 0, err
	}
	return count, nil
}

func run() (resultErr error) {
	program := flag.String("engine-program", "", "verified project-local native engine")
	rootArg := flag.String("root", "", "new absolute probe directory inside .state")
	allowed := flag.Bool("allow-existing-account", false, "explicitly allow bounded real model turns using existing local account")
	model := flag.String("model", "gpt-6.1-sol", "exact model for these development turns")
	flag.Parse()
	if !*allowed || !filepath.IsAbs(*rootArg) || !filepath.IsAbs(*program) {
		return errors.New("explicit existing-account authorization and absolute project-local paths are required")
	}
	state, err := filepath.Abs(".state")
	if err != nil {
		return err
	}
	state, err = filepath.EvalSymlinks(state)
	if err != nil {
		return err
	}
	if filepath.Dir(*rootArg) != state || !strings.HasPrefix(filepath.Base(*rootArg), "real-executor-") {
		return errors.New("probe root must be a new direct child of this project's .state")
	}
	if _, err = runtimefiles.EngineResources(*program, engine.CompatibleVersion); err != nil {
		return err
	}
	if err = os.Mkdir(*rootArg, 0700); err != nil {
		return errors.New("probe root already exists or cannot be created; original inputs were not resubmitted")
	}
	root := *rootArg
	report := probe{Root: root, At: protocol.Now(), State: "preparing", EngineVersion: engine.CompatibleVersion, Model: *model, Phases: []phase{}}
	modelHome := os.Getenv("CODEX_HOME")
	if modelHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		modelHome = filepath.Join(userHome, ".codex")
	}
	configPath := filepath.Join(modelHome, "config.toml")
	report.ConfigSHA256, err = hashConfig(configPath)
	if err != nil {
		return err
	}
	defer func() {
		after, err := hashConfig(configPath)
		report.GlobalConfigUnchanged = err == nil && after == report.ConfigSHA256
		report.State = "succeeded"
		if resultErr != nil {
			report.State = "failed"
			report.Error = resultErr.Error()
		}
		if err = writeReport(&report); err != nil && resultErr == nil {
			resultErr = err
		}
		fmt.Println(string(protocol.JSON(report)))
	}()
	workspace := filepath.Join(root, "workspace")
	data := filepath.Join(root, "data")
	for _, path := range []string{workspace, data} {
		if err = os.Mkdir(path, 0700); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	diagnosticFile, err := os.OpenFile(filepath.Join(root, "native-diagnostics.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer diagnosticFile.Close()
	diagnostics := &boundedLog{file: diagnosticFile, remaining: 1 << 20}
	var native *engine.Codex
	var controller *executor.Executor
	var database *store.Store
	var lease *lifecycle.Lock
	closeController := func() error {
		var closeErr error
		if controller != nil {
			closeErr = controller.Close()
			controller = nil
			native = nil
		} else if native != nil {
			closeErr = native.Close()
			native = nil
		}
		if database != nil {
			err := database.Close()
			if closeErr == nil {
				closeErr = err
			}
			database = nil
		}
		if lease != nil {
			err := lease.Close()
			if closeErr == nil {
				closeErr = err
			}
			lease = nil
		}
		return closeErr
	}
	defer func() {
		if err := closeController(); resultErr == nil {
			resultErr = err
		}
	}()
	c := config.Config{DeviceID: "real-executor-risk-" + protocol.ID(), Name: "owned real-model development check", DataDir: data, Projects: []protocol.Project{{ID: "risk", Name: "owned fixture", Path: workspace}}}
	openController := func() error {
		var err error
		lease, err = lifecycle.Acquire(c.DataDir)
		if err != nil {
			return err
		}
		native, report.ExternalToolSetsDisabled, err = nativeForProbe(ctx, *program, workspace, diagnostics)
		if err != nil {
			return err
		}
		report.HooksDisabled = true
		var account struct {
			Account *struct {
				Type     string `json:"type"`
				PlanType string `json:"planType"`
			} `json:"account"`
		}
		if err = native.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
			return err
		}
		if account.Account == nil || account.Account.Type != "chatgpt" {
			return errors.New("existing local ChatGPT account is not available; no login was started")
		}
		report.SignedIn, report.AccountType, report.PlanType = true, account.Account.Type, account.Account.PlanType
		database, err = store.Open(filepath.Join(c.DataDir, "executor.db"))
		if err != nil {
			return err
		}
		controller, err = executor.New(c, database, native, nil)
		return err
	}
	if err = openController(); err != nil {
		return err
	}
	assets, err := attachments.New(filepath.Join(data, "attachments"))
	if err != nil {
		return err
	}
	value := protocol.ID()
	textAttachment, err := assets.SaveTarget("attachment-token.json", bytes.NewReader(protocol.JSON(map[string]string{"probeValue": value})), "risk-input", c.DeviceID, "risk")
	if err != nil {
		return err
	}
	colors := []string{"red", "green", "blue", "yellow"}
	palette := map[string]color.RGBA{"red": {255, 0, 0, 255}, "green": {0, 190, 0, 255}, "blue": {0, 0, 255, 255}, "yellow": {255, 255, 0, 255}}
	var shuffle [4]byte
	if _, err = rand.Read(shuffle[:]); err != nil {
		return err
	}
	for i := 3; i > 0; i-- {
		j := int(shuffle[i]) % (i + 1)
		colors[i], colors[j] = colors[j], colors[i]
	}
	picture := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			index := 0
			if x >= 128 {
				index++
			}
			if y >= 128 {
				index += 2
			}
			picture.SetRGBA(x, y, palette[colors[index]])
		}
	}
	var imageBytes bytes.Buffer
	if err = png.Encode(&imageBytes, picture); err != nil {
		return err
	}
	imageAttachment, err := assets.SaveTarget("quadrants.png", bytes.NewReader(imageBytes.Bytes()), "risk-input", c.DeviceID, "risk")
	if err != nil {
		return err
	}
	p := protocol.Policy{Mode: "edit", NoTests: true, Model: *model, Instructions: "只处理用户指定的自有附件和当前项目文件，不运行测试、构建或其他项目流程。仅使用内置命令/文件工具；不调用外部服务，不请求扩展权限。"}
	created := controller.Handle(ctx, protocol.Command{ID: protocol.ID(), SourceID: "risk-input", DeviceID: c.DeviceID, ProjectID: "risk", Kind: "create", Title: "MewDex owned attachment development check", Policy: &p})
	if created.State != "completed" || created.Session == nil {
		return fmt.Errorf("create: %s: %s", created.State, created.Error)
	}
	session := *created.Session
	report.SessionID = session.ID
	for round := 0; round < 2; round++ {
		if round == 1 {
			if err = closeController(); err != nil {
				return err
			}
			restored := filepath.Join(root, "restored-data")
			if err = os.Mkdir(restored, 0700); err != nil {
				return err
			}
			if _, err = backup.CopyVerified(ctx, data, restored, []backup.Selection{{Path: "executor.db", Name: "executor.db"}, {Path: "attachments", Name: "attachments"}}, backup.Options{}); err != nil {
				return err
			}
			copy, err := store.Open(filepath.Join(restored, "executor.db"))
			if err != nil {
				return err
			}
			err = copy.PrepareRestore(ctx, c.DeviceID, true)
			copy.Close()
			if err != nil {
				return err
			}
			c.DataDir = restored
			if err = openController(); err != nil {
				return err
			}
			// The owned restored copy is explicitly resumed after its frozen,
			// known completed first turn. No original input is replayed.
			resumed := controller.Handle(ctx, protocol.Command{ID: protocol.ID(), SourceID: "risk-input", DeviceID: c.DeviceID, SessionID: session.ID, Kind: "resume"})
			if resumed.State != "completed" || resumed.Session == nil {
				return fmt.Errorf("resume restored fixture: %s", resumed.Error)
			}
			session = *resumed.Session
		}
		output := fmt.Sprintf("result-%d.json", round+1)
		prompt := fmt.Sprintf("这是功能开发检查，只操作当前项目。请使用内置命令工具读取附件 attachment-token.json，取得其中 probeValue；观察所附图片的左上、右上、左下、右下四个象限，颜色使用英文 red/green/blue/yellow。创建当前项目文件 %s，JSON 仅有 probeValue、colors（上述顺序的四项数组）、sourcePath（实际读取的附件绝对路径）三个字段。probeValue 必须来自附件，不猜测。不要运行测试或访问网络。完成后简短回复。", output)
		inputs := []protocol.Input{{Type: "text", Text: prompt}, {Type: "file", AttachmentID: textAttachment.ID}, {Type: "image", AttachmentID: imageAttachment.ID}}
		if round == 1 {
			inputs = []protocol.Input{{Type: "text", Text: fmt.Sprintf("当前对话的数据位置已迁移。请使用内置命令工具读取 MewDex 提供的历史附件索引，再实际读取索引中 attachment-token.json 的当前路径；不要使用上回合结果文件或只凭记忆填写。继续使用历史图像的四象限颜色。创建当前项目文件 %s，字段仍只有 probeValue、colors、sourcePath，记录本次实际读取路径。不要运行测试，不访问网络，不请求扩权。", output)}}
		}
		commandID := protocol.ID()
		report.Phases = append(report.Phases, phase{Name: []string{"file-and-image", "restored-history"}[round], CommandID: commandID, State: "submitting"})
		if err = writeReport(&report); err != nil {
			return err
		}
		started := controller.Handle(ctx, protocol.Command{ID: commandID, SourceID: "risk-input", DeviceID: c.DeviceID, SessionID: session.ID, Kind: "start", Input: inputs})
		if started.State != "completed" || started.Session == nil {
			return fmt.Errorf("%s submit: %s: %s", report.Phases[round].Name, started.State, started.Error)
		}
		current := &report.Phases[round]
		current.EngineID, current.TurnID, current.ScopeID = started.Session.EngineID, started.Session.TurnID, started.Session.ExecutionScopeID
		current.State = "running"
		if err = writeReport(&report); err != nil {
			return err
		}
		deadline := time.NewTimer(150 * time.Second)
		ticker := time.NewTicker(150 * time.Millisecond)
		for {
			var latest protocol.Session
			if err = database.Get("sessions", session.ID, &latest); err != nil {
				ticker.Stop()
				deadline.Stop()
				return err
			}
			session = latest
			requests, err := store.List[protocol.Interaction](database, "interactions")
			if err != nil {
				ticker.Stop()
				deadline.Stop()
				return err
			}
			for _, request := range requests {
				if request.SessionID == session.ID && request.State == "pending" {
					ticker.Stop()
					deadline.Stop()
					return errors.New("real development turn requested an unapproved interaction; no reply was sent")
				}
			}
			if session.State != "running" && session.State != "stopping" && session.StopStatus != "background-running" && session.StopStatus != "processes-unchecked" {
				break
			}
			select {
			case <-ctx.Done():
				ticker.Stop()
				deadline.Stop()
				return ctx.Err()
			case <-deadline.C:
				ticker.Stop()
				return errors.New("original real-model turn remains unconfirmed; no automatic retry")
			case <-ticker.C:
			}
		}
		ticker.Stop()
		deadline.Stop()
		if session.State != "idle" || session.Frozen {
			return fmt.Errorf("model turn did not reach confirmed idle state (%s)", session.State)
		}
		var result fixtureResult
		if err = boundedJSON(workspace, output, &result); err != nil {
			return err
		}
		if result.ProbeValue != value || len(result.Colors) != len(colors) {
			return errors.New("real model did not read the attachment or image correctly")
		}
		for i, name := range colors {
			if strings.ToLower(strings.TrimSpace(result.Colors[i])) != name {
				return errors.New("real model image quadrant result did not match its fixture")
			}
		}
		var contextSnapshot struct {
			AttachmentIndex string `json:"attachmentIndex"`
		}
		if err = database.Get("turn_contexts", commandID, &contextSnapshot); err != nil {
			return err
		}
		var after int64
		for {
			events, err := database.Events(after, session.ID, 1000)
			if err != nil {
				return err
			}
			for _, event := range events {
				after = event.Sequence
				if event.Kind != "engine.item/completed" {
					continue
				}
				var item struct {
					TurnID string `json:"turnId"`
					Item   struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"item"`
				}
				if json.Unmarshal(event.Payload, &item) == nil && item.TurnID == current.TurnID && item.Item.Type == "commandExecution" {
					current.NativeCommandItems++
					if contextSnapshot.AttachmentIndex != "" && strings.Contains(item.Item.Command, filepath.Base(contextSnapshot.AttachmentIndex)) {
						current.HistoryIndexRead = true
					}
				}
			}
			if len(events) < 1000 {
				break
			}
		}
		if current.NativeCommandItems == 0 {
			return errors.New("no actual native command was observed for the real model turn")
		}
		if round == 1 {
			if !current.HistoryIndexRead || contextSnapshot.AttachmentIndex == "" || filepath.Dir(result.SourcePath) != filepath.Dir(contextSnapshot.AttachmentIndex) {
				return errors.New("restored model did not read the current historical attachment index")
			}
			if report.Phases[0].EngineID != current.EngineID || report.Phases[0].ScopeID == current.ScopeID {
				return errors.New("restored fixture did not preserve the original conversation with a new execution scope")
			}
		}
		current.State, current.OutputVerified = "completed", true
		if err = writeReport(&report); err != nil {
			return err
		}
	}
	return nil
}
