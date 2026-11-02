package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

// RequestView translates the pinned engine protocol to the input-client contract.
// It deliberately offers one-request decisions, never implicit session-wide grants.
func RequestView(n Notification, session string) (protocol.Interaction, error) {
	var p struct {
		TurnID      string          `json:"turnId"`
		Reason      string          `json:"reason"`
		Command     string          `json:"command"`
		Cwd         string          `json:"cwd"`
		GrantRoot   string          `json:"grantRoot"`
		Permissions json.RawMessage `json:"permissions"`
		IsBlocking  bool            `json:"isBlocking"`
		Questions   []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsSecret bool   `json:"isSecret"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
		Mode            string          `json:"mode"`
		Message         string          `json:"message"`
		URL             string          `json:"url"`
		ServerName      string          `json:"serverName"`
		RequestedSchema json.RawMessage `json:"requestedSchema"`
	}
	if err := json.Unmarshal(n.Params, &p); err != nil {
		return protocol.Interaction{}, err
	}
	v := protocol.Interaction{ID: protocol.ID(), SessionID: session, TurnID: p.TurnID, State: "pending", Blocking: true, CanAccept: true, Message: p.Reason, Cwd: p.Cwd, Questions: []protocol.InputQuestion{}, CreatedAt: protocol.Now()}
	switch n.Method {
	case "item/commandExecution/requestApproval":
		v.Kind, v.Title, v.Command = "command", "确认执行命令", p.Command
	case "item/fileChange/requestApproval":
		v.Kind, v.Title, v.Details = "files", "确认文件修改", p.GrantRoot
	case "item/permissions/requestApproval":
		v.Kind, v.Title = "permissions", "确认本回合的额外权限"
		v.CanAccept = false // The executor must validate paths against its scope.
		grant, err := RequestedPermissions(n)
		if err != nil {
			v.Details = err.Error()
		} else {
			v.Permissions = &grant
		}
	case "item/tool/requestUserInput":
		v.Kind, v.Title, v.Blocking = "questions", "补充任务信息", p.IsBlocking
		seen := map[string]bool{}
		for _, q := range p.Questions {
			if q.ID == "" || seen[q.ID] {
				return v, errors.New("engine question IDs are invalid")
			}
			seen[q.ID] = true
			question := protocol.InputQuestion{ID: q.ID, Title: q.Header, Description: q.Question, Kind: "text", Required: true, AllowText: true, Options: []protocol.InputOption{}}
			if q.IsSecret {
				question.Kind = "secret"
			}
			for _, o := range q.Options {
				question.Options = append(question.Options, protocol.InputOption{Value: o.Label, Label: o.Label, Description: o.Description})
			}
			v.Questions = append(v.Questions, question)
		}
		if len(v.Questions) == 0 {
			return v, errors.New("engine supplied no questions")
		}
	case "mcpServer/elicitation/request":
		v.Title, v.Message = "来自 "+p.ServerName+" 的请求", p.Message
		if p.Mode == "url" {
			u, err := url.Parse(p.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return v, errors.New("external request requires an HTTPS URL without embedded credentials")
			}
			v.Kind, v.URL = "link", p.URL
		} else {
			v.Kind = "form"
			var err error
			v.Questions, err = formQuestions(p.RequestedSchema)
			if err != nil {
				v.CanAccept = false
				v.Details = err.Error()
			}
		}
	default:
		return v, fmt.Errorf("unsupported engine request method: %s", n.Method)
	}
	return v, nil
}

type formField struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Format      string   `json:"format"`
	Enum        []string `json:"enum"`
	EnumNames   []string `json:"enumNames"`
	OneOf       []struct {
		Const string `json:"const"`
		Title string `json:"title"`
	} `json:"oneOf"`
	Items     *formField      `json:"items"`
	Default   json.RawMessage `json:"default"`
	Minimum   *float64        `json:"minimum"`
	Maximum   *float64        `json:"maximum"`
	MinLength *int            `json:"minLength"`
	MaxLength *int            `json:"maxLength"`
	MinItems  *int            `json:"minItems"`
	MaxItems  *int            `json:"maxItems"`
}

func formQuestions(raw json.RawMessage) ([]protocol.InputQuestion, error) {
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(raw, &schema) != nil || schema.Type != "object" {
		return nil, errors.New("此表单使用了暂不支持的结构，可拒绝本次请求")
	}
	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 64 {
		return nil, errors.New("表单字段过多")
	}
	required := map[string]bool{}
	for _, key := range schema.Required {
		required[key] = true
	}
	questions := []protocol.InputQuestion{}
	for _, key := range keys {
		var f formField
		if err := json.Unmarshal(schema.Properties[key], &f); err != nil {
			return nil, errors.New("表单包含暂不支持的字段")
		}
		q := protocol.InputQuestion{ID: key, Title: f.Title, Description: f.Description, Kind: f.Type, Format: f.Format, Required: required[key], Default: f.Default, Options: []protocol.InputOption{}, Minimum: f.Minimum, Maximum: f.Maximum, MinLength: f.MinLength, MaxLength: f.MaxLength}
		if q.Title == "" {
			q.Title = key
		}
		switch f.Type {
		case "string":
			q.Kind = "text"
			if f.Format == "password" {
				q.Kind = "secret"
			}
		case "integer", "number", "boolean":
		case "array":
			if f.Items == nil || (len(f.Items.Enum) == 0 && len(f.Items.OneOf) == 0) {
				return nil, errors.New("表单数组需要明确的选项")
			}
			q.Kind, q.MinLength, q.MaxLength = "multi", f.MinItems, f.MaxItems
			f.Enum, f.EnumNames, f.OneOf = f.Items.Enum, f.Items.EnumNames, f.Items.OneOf
		default:
			return nil, fmt.Errorf("暂不支持表单字段 %s 的类型", key)
		}
		for i, value := range f.Enum {
			label := value
			if i < len(f.EnumNames) {
				label = f.EnumNames[i]
			}
			q.Options = append(q.Options, protocol.InputOption{Value: value, Label: label})
		}
		for _, option := range f.OneOf {
			label := option.Title
			if label == "" {
				label = option.Const
			}
			q.Options = append(q.Options, protocol.InputOption{Value: option.Const, Label: label})
		}
		if len(q.Options) > 0 && q.Kind == "text" {
			q.Kind = "select"
		}
		questions = append(questions, q)
	}
	return questions, nil
}

func ReplyFor(n Notification, view protocol.Interaction, reply protocol.InteractionReply) (json.RawMessage, error) {
	if reply.Action != "accept" && reply.Action != "decline" && reply.Action != "cancel" {
		return nil, errors.New("unsupported reply action")
	}
	if reply.Action == "accept" && !view.CanAccept {
		return nil, errors.New("this request cannot be accepted by this client version")
	}
	switch n.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return protocol.JSON(map[string]string{"decision": reply.Action}), nil
	case "item/permissions/requestApproval":
		if reply.Action == "accept" {
			if view.Permissions == nil {
				return nil, errors.New("the executor has not confirmed these permissions")
			}
			return permissionReply(*view.Permissions), nil
		}
		return permissionReply(protocol.PermissionGrant{}), nil
	case "item/tool/requestUserInput":
		if reply.Action != "accept" {
			return nil, errors.New("answer these questions or stop the task")
		}
		answers := map[string]any{}
		for _, q := range view.Questions {
			values := reply.Answers[q.ID]
			if len(values) != 1 || strings.TrimSpace(values[0]) == "" || len(values[0]) > 65536 {
				return nil, fmt.Errorf("question %s requires one answer", q.ID)
			}
			answers[q.ID] = map[string]any{"answers": values}
		}
		if len(reply.Answers) != len(view.Questions) {
			return nil, errors.New("answers contain unknown questions")
		}
		return protocol.JSON(map[string]any{"answers": answers}), nil
	case "mcpServer/elicitation/request":
		content := map[string]json.RawMessage{}
		if reply.Action == "accept" && view.Kind == "form" {
			if err := validateForm(view.Questions, reply.Values); err != nil {
				return nil, err
			}
			content = reply.Values
		}
		return protocol.JSON(map[string]any{"action": reply.Action, "content": content}), nil
	}
	return nil, errors.New("unsupported engine request")
}

func validateForm(questions []protocol.InputQuestion, values map[string]json.RawMessage) error {
	allowed := map[string]bool{}
	for _, q := range questions {
		allowed[q.ID] = true
		raw, found := values[q.ID]
		if !found || string(raw) == "null" {
			if q.Required {
				return fmt.Errorf("%s is required", q.Title)
			}
			continue
		}
		bad := func() error { return fmt.Errorf("%s has an invalid value", q.Title) }
		switch q.Kind {
		case "text", "secret", "select":
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return bad()
			}
			length := utf8.RuneCountInString(text)
			if q.Required && length == 0 || q.MinLength != nil && length < *q.MinLength || q.MaxLength != nil && length > *q.MaxLength || length > 65536 {
				return bad()
			}
			if q.Kind == "select" && !hasOption(q, text) {
				return bad()
			}
		case "number", "integer":
			var value float64
			if json.Unmarshal(raw, &value) != nil {
				return bad()
			}
			if q.Kind == "integer" && value != math.Trunc(value) || q.Minimum != nil && value < *q.Minimum || q.Maximum != nil && value > *q.Maximum {
				return bad()
			}
		case "boolean":
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return bad()
			}
		case "multi":
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return bad()
			}
			if q.MinLength != nil && len(value) < *q.MinLength || q.MaxLength != nil && len(value) > *q.MaxLength {
				return bad()
			}
			seen := map[string]bool{}
			for _, item := range value {
				if seen[item] || !hasOption(q, item) {
					return bad()
				}
				seen[item] = true
			}
		default:
			return bad()
		}
	}
	for key := range values {
		if !allowed[key] {
			return errors.New("form contains an unknown field")
		}
	}
	return nil
}
func hasOption(q protocol.InputQuestion, value string) bool {
	for _, o := range q.Options {
		if o.Value == value {
			return true
		}
	}
	return false
}
