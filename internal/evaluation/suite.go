// Package evaluation runs versioned tasks through the normal Hub input API and
// grades saved conversation evidence. It never executes tools on the client.
package evaluation

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"mewdex/internal/protocol"
)

type Suite struct {
	Format    int     `json:"format"`
	Name      string  `json:"name"`
	ProjectID string  `json:"projectId"`
	Models    []Model `json:"models"`
	Cases     []Case  `json:"cases"`
}

type Model struct {
	ID              string `json:"id"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type Case struct {
	ID                 string          `json:"id"`
	Prompt             string          `json:"prompt"`
	Policy             protocol.Policy `json:"policy"`
	Checks             []Check         `json:"checks"`
	SequentialCommands bool            `json:"sequentialCommands,omitempty"`
}

type Check struct {
	Kind           string   `json:"kind"`
	Value          string   `json:"value,omitempty"`
	ToolType       string   `json:"toolType,omitempty"`
	Operation      string   `json:"operation,omitempty"`
	Count          *int     `json:"count,omitempty"`
	DeviceID       string   `json:"deviceId,omitempty"`
	ProjectID      string   `json:"projectId,omitempty"`
	Command        []string `json:"command,omitempty"`
	OutputContains string   `json:"outputContains,omitempty"`
	Status         string   `json:"status,omitempty"`
	ExitCode       *int     `json:"exitCode,omitempty"`
}

func (s Suite) Validate() error {
	if s.Format != 1 || strings.TrimSpace(s.Name) == "" || len(s.Name) > 200 || s.ProjectID == "" || len(s.Models) < 1 || len(s.Models) > 4 || len(s.Cases) < 1 || len(s.Cases) > 20 {
		return errors.New("evaluation requires format 1, a project, 1-4 explicit models and 1-20 cases")
	}
	models, cases := map[string]bool{}, map[string]bool{}
	for _, model := range s.Models {
		key := model.ID + "\x00" + model.ReasoningEffort
		if model.ID == "" || len(model.ID) > 200 || len(model.ReasoningEffort) > 64 || models[key] {
			return errors.New("evaluation model identities must be explicit, bounded and unique")
		}
		models[key] = true
	}
	identifier := regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	for _, item := range s.Cases {
		if !identifier.MatchString(item.ID) || cases[item.ID] || strings.TrimSpace(item.Prompt) == "" || len(item.Prompt) > 32768 || len(item.Checks) < 1 || len(item.Checks) > 20 {
			return fmt.Errorf("invalid evaluation case %q", item.ID)
		}
		cases[item.ID] = true
		if item.Policy.Model != "" || item.Policy.ReasoningEffort != "" {
			return errors.New("case model settings belong in the suite model matrix")
		}
		if !slices.Contains([]string{"", "read-only", "workspace", "workspace-network"}, item.Policy.AccessMode) {
			return errors.New("unsupported evaluation access mode")
		}
		for _, check := range item.Checks {
			if err := validateCheck(check); err != nil {
				return fmt.Errorf("case %s %w", item.ID, err)
			}
		}
		if item.SequentialCommands && !slices.ContainsFunc(item.Checks, func(check Check) bool { return check.Kind == "command" }) {
			return fmt.Errorf("case %s requires command checks for sequential execution", item.ID)
		}
	}
	return nil
}

func validateCheck(check Check) error {
	switch check.Kind {
	case "assistant.equals", "assistant.contains":
		if check.Value != "" && len(check.Value) <= 32768 {
			return nil
		}
	case "tool.count":
		if slices.Contains([]string{"commandExecution", "fileChange", "mcpToolCall", "computerUse"}, check.ToolType) && check.Count != nil && *check.Count >= 0 && *check.Count <= 100 {
			return nil
		}
	case "command":
		if check.DeviceID != "" && check.ProjectID != "" && len(check.Command) > 0 && len(check.Command) <= 100 && check.Count != nil && *check.Count > 0 && *check.Count <= 100 && slices.Contains([]string{"", "completed", "failed", "stopped"}, check.Status) {
			return nil
		}
	case "computer":
		if check.DeviceID != "" && check.ProjectID != "" && slices.Contains([]string{"request", "status", "observe", "act", "stop"}, check.Operation) && check.Count != nil && *check.Count > 0 && *check.Count <= 100 {
			return nil
		}
	}
	return errors.New("unsupported or incomplete evaluation check")
}
