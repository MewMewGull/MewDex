//go:build windows

package startup

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

type nativeBackend struct {
	powershell string
	sid        string
	account    string
}

func Native() (Backend, error) {
	directory, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	account, domain, _, _ := user.User.Sid.LookupAccount("")
	if domain != "" {
		account = domain + "\\" + account
	}
	return &nativeBackend{powershell: filepath.Join(directory, "WindowsPowerShell", "v1.0", "powershell.exe"), sid: user.User.Sid.String(), account: account}, nil
}

func (*nativeBackend) Scope() string { return "user-login" }

type taskXML struct {
	XMLName   xml.Name `xml:"Task"`
	Namespace string   `xml:"xmlns,attr"`
	Version   string   `xml:"version,attr"`
	Source    string   `xml:"RegistrationInfo>Source"`
	Author    string   `xml:"RegistrationInfo>Author"`
	Trigger   struct {
		Enabled *bool  `xml:"Enabled"`
		UserID  string `xml:"UserId"`
	} `xml:"Triggers>LogonTrigger"`
	Principal struct {
		ID        string `xml:"id,attr"`
		UserID    string `xml:"UserId"`
		LogonType string `xml:"LogonType"`
		RunLevel  string `xml:"RunLevel"`
	} `xml:"Principals>Principal"`
	Settings struct {
		MultipleInstances  string `xml:"MultipleInstancesPolicy"`
		DisallowBattery    bool   `xml:"DisallowStartIfOnBatteries"`
		StopBattery        bool   `xml:"StopIfGoingOnBatteries"`
		AllowHardTerminate bool   `xml:"AllowHardTerminate"`
		StartWhenAvailable bool   `xml:"StartWhenAvailable"`
		RunOnlyIfNetwork   bool   `xml:"RunOnlyIfNetworkAvailable"`
		AllowStartOnDemand bool   `xml:"AllowStartOnDemand"`
		Enabled            bool   `xml:"Enabled"`
		Hidden             bool   `xml:"Hidden"`
		ExecutionTimeLimit string `xml:"ExecutionTimeLimit"`
		RestartOnFailure   struct {
			Interval string `xml:"Interval"`
			Count    int    `xml:"Count"`
		} `xml:"RestartOnFailure"`
	} `xml:"Settings"`
	Actions struct {
		Context string `xml:"Context,attr"`
		Exec    struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
			Directory string `xml:"WorkingDirectory"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func (b *nativeBackend) render(d Definition) ([]byte, error) {
	if len(utf16.Encode([]rune(d.Program))) > 259 || len(utf16.Encode([]rune(d.Root))) > 259 {
		return nil, errors.New("Windows startup requires an installation path shorter than 260 characters")
	}
	value := taskXML{Namespace: "http://schemas.microsoft.com/windows/2004/02/mit/task", Version: "1.3", Source: "MewDex:" + d.Owner, Author: "MewDex"}
	enabled := true
	value.Trigger.Enabled, value.Trigger.UserID = &enabled, b.sid
	value.Principal.ID, value.Principal.UserID, value.Principal.LogonType, value.Principal.RunLevel = "MewDexUser", b.sid, "InteractiveToken", "LeastPrivilege"
	value.Settings.MultipleInstances = "IgnoreNew"
	value.Settings.AllowHardTerminate, value.Settings.StartWhenAvailable, value.Settings.AllowStartOnDemand, value.Settings.Enabled, value.Settings.Hidden = true, true, true, true, true
	value.Settings.ExecutionTimeLimit = "PT0S"
	value.Settings.RestartOnFailure.Interval, value.Settings.RestartOnFailure.Count = "PT1M", 3
	value.Actions.Context = "MewDexUser"
	value.Actions.Exec.Command, value.Actions.Exec.Directory = d.Program, d.Root
	arguments := make([]string, len(d.Arguments))
	for i, arg := range d.Arguments {
		arguments[i] = syscall.EscapeArg(arg)
	}
	value.Actions.Exec.Arguments = strings.Join(arguments, " ")
	return xml.Marshal(value)
}

// The script is constant; all installation paths and XML arrive as JSON over
// stdin. No password, S4U logon, execution-policy change or elevated principal.
const schedulerScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
try {
  $request = [Console]::In.ReadToEnd() | ConvertFrom-Json
  $service = New-Object -ComObject 'Schedule.Service'
  $service.Connect()
  $folder = $service.GetFolder('\')
  function Read-OwnedTask {
    try { return $folder.GetTask($request.name) }
    catch {
      $cause = $_.Exception
      while ($cause.InnerException) { $cause = $cause.InnerException }
      $code = $cause.HResult
      if ($code -eq -2147024894 -or $code -eq -2147216625) { return $null }
      throw
    }
  }
  $task = Read-OwnedTask
  if ($task -and $request.operation -ne 'inspect') {
    $document = [xml]$task.Xml
    if ([string]$document.Task.RegistrationInfo.Source -ne [string]$request.source) {
      throw 'The existing startup task belongs to another installation'
    }
  }
  switch ($request.operation) {
    'inspect' { }
    'install' { $flags = if ($task) { 4 } else { 2 }; $null = $folder.RegisterTask($request.name, [string]$request.xml, $flags, $null, $null, 3, $null) }
    'remove' { if ($task) { $folder.DeleteTask($request.name, 0) } }
    'start' { if (-not $task) { throw 'Startup task is missing' }; $null = $task.Run($null) }
    'stop' {
      if ($task) { $task.Stop(0) }
      else {
        foreach ($instance in $service.GetRunningTasks(0)) {
          if ([string]$instance.Path -eq ('\' + [string]$request.name)) { $instance.Stop() }
        }
      }
    }
    default { throw 'Unsupported startup operation' }
  }
  $task = Read-OwnedTask
  if ($task) {
    $raw = [string]$task.Xml
    $document = [xml]$raw
    $result = @{ ok=$true; present=$true; owner=([string]$document.Task.RegistrationInfo.Source); enabled=[bool]$task.Enabled; running=([int]$task.State -eq 4); raw=$raw }
  } else { $result = @{ ok=$true; present=$false; enabled=$false; running=$false } }
  [Console]::Write(($result | ConvertTo-Json -Compress))
} catch {
  [Console]::Write((@{ok=$false; code=$_.Exception.HResult; message=$_.Exception.Message} | ConvertTo-Json -Compress))
}
`

func encodedScript(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(raw[2*i:], unit)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func (b *nativeBackend) call(ctx context.Context, operation string, d Definition) (Entry, error) {
	var body []byte
	var err error
	if operation == "install" {
		body, err = b.render(d)
		if err != nil {
			return Entry{}, err
		}
	}
	request, _ := json.Marshal(map[string]string{"operation": operation, "name": d.Name, "source": "MewDex:" + d.Owner, "xml": string(body)})
	raw, err := run(ctx, b.powershell, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedScript(schedulerScript)}, request)
	if err != nil {
		return Entry{}, errors.New("Windows startup manager could not be reached")
	}
	var result struct {
		OK      bool   `json:"ok"`
		Present bool   `json:"present"`
		Owner   string `json:"owner"`
		Enabled bool   `json:"enabled"`
		Running bool   `json:"running"`
		Raw     string `json:"raw"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return Entry{}, errors.New("Windows startup manager returned an invalid result")
	}
	if !result.OK {
		return Entry{}, errors.New("Windows startup operation failed: " + result.Message)
	}
	match := result.Present && b.matches(result.Raw, d)
	owner := ""
	if strings.HasPrefix(result.Owner, "MewDex:") {
		owner = strings.TrimPrefix(result.Owner, "MewDex:")
	}
	return Entry{Present: result.Present, Owner: owner, Enabled: result.Enabled, Running: result.Running, Matches: match, Raw: []byte(result.Raw)}, nil
}

func (b *nativeBackend) matches(raw string, d Definition) bool {
	var current taskXML
	// Task Scheduler returns a UTF-16 declaration in a .NET string. JSON has
	// already converted that string to UTF-8; omit only its declaration to parse.
	parseRaw := strings.TrimSpace(raw)
	if strings.HasPrefix(parseRaw, "<?xml") {
		if _, rest, found := strings.Cut(parseRaw, "?>"); found {
			parseRaw = rest
		}
	}
	if xml.Unmarshal([]byte(parseRaw), &current) == nil {
		expected, renderErr := b.render(d)
		var wanted taskXML
		if renderErr == nil && xml.Unmarshal(expected, &wanted) == nil {
			triggerUser := strings.EqualFold(current.Trigger.UserID, b.sid) || (b.account != "" && strings.EqualFold(current.Trigger.UserID, b.account))
			return current.Actions.Exec == wanted.Actions.Exec && current.Principal.UserID == wanted.Principal.UserID && current.Principal.LogonType == "InteractiveToken" && (current.Principal.RunLevel == "LeastPrivilege" || current.Principal.RunLevel == "") && triggerUser && (current.Trigger.Enabled == nil || *current.Trigger.Enabled) && current.Settings.ExecutionTimeLimit == "PT0S"
		}
	}
	return false
}

func (b *nativeBackend) Inspect(ctx context.Context, d Definition) (Entry, error) {
	return b.call(ctx, "inspect", d)
}
func (b *nativeBackend) Install(ctx context.Context, d Definition) error {
	_, err := b.call(ctx, "install", d)
	return err
}
func (b *nativeBackend) Remove(ctx context.Context, d Definition) error {
	_, err := b.call(ctx, "remove", d)
	return err
}
func (b *nativeBackend) Start(ctx context.Context, d Definition) error {
	_, err := b.call(ctx, "start", d)
	return err
}
func (b *nativeBackend) Stop(ctx context.Context, d Definition) error {
	_, err := b.call(ctx, "stop", d)
	return err
}
