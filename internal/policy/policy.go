package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"mewdex/internal/protocol"
)

func Normalize(p protocol.Policy) (protocol.Policy, error) {
	if p.AccessMode == "workspace" {
		p.AccessMode = ""
	}
	if p.AccessMode != "" && p.AccessMode != "read-only" && p.AccessMode != "workspace-network" {
		return p, errors.New("access mode must be read-only, workspace or workspace-network")
	}
	if p.CollaborationMode == "default" {
		p.CollaborationMode = ""
	}
	if p.CollaborationMode != "" && p.CollaborationMode != "plan" {
		return p, errors.New("collaboration mode must be default or plan")
	}
	if len(p.Instructions) > 256*1024 || len(p.Completion) > 64*1024 || p.BaseInstructions != nil && len(*p.BaseInstructions) > 256*1024 {
		return p, errors.New("instructions exceed the supported size")
	}
	if len(p.Model) > 200 || strings.ContainsAny(p.Model, "\r\n\x00") {
		return p, errors.New("invalid model identifier")
	}
	if len(p.ReasoningEffort) > 64 || strings.ContainsAny(p.ReasoningEffort, " \t\r\n\x00") {
		return p, errors.New("invalid reasoning effort")
	}
	if p.Mode == "" {
		p.Mode = "focused"
	}
	if p.Mode != "edit" && p.Mode != "focused" && p.Mode != "full" {
		return p, errors.New("policy.mode must be edit, focused or full")
	}
	return p, nil
}

func Instructions(p protocol.Policy) string {
	var b strings.Builder
	b.WriteString("以下为本回合最新的 MewDex 任务策略，替代会话中更早的 MewDex 任务策略。\n")
	b.WriteString("完成当前用户要求，保持改动范围。不要为假设中的未来需求添加兼容层、回退、抽象或额外流程。保留无关本地修改。达到约定完成条件后结束，如实说明未执行的检查。\n")
	switch p.Mode {
	case "edit":
		b.WriteString("本任务为直接修改模式：检查相关差异，按需做必要的局部检查，不主动扩大验证。\n")
	case "full":
		b.WriteString("本任务为完整验证模式：仅执行用户或项目为本任务选定的构建、启动、打包和回归范围。\n")
	default:
		b.WriteString("本任务为局部验证模式：只检查受影响模块；运行时缺陷仅进入指定场景和目标操作链路。小改动不自动全量编译、打包、启动编辑器或全量回归。\n")
	}
	if p.NoTests {
		b.WriteString("用户明确要求本任务不要测试，不自行追加测试、构建、编辑器启动或回归；交付时记录未验证范围。\n")
	}
	b.WriteString("仅相关变更、具体失败证据或明确用户要求才扩大验证；相关内容和环境未变时复用已通过的检查。\n")
	if p.Completion != "" {
		b.WriteString("本任务完成条件：" + p.Completion + "\n")
	}
	if p.Instructions != "" {
		b.WriteString(p.Instructions)
	}
	if p.CollaborationMode == "plan" {
		b.WriteString("\n当前协作方式为方案讨论：先理解目标、查阅必要资料并提出可审阅方案，需要用户决策的信息通过问题请求补充。本回合不实施代码或配置修改，不主动执行构建、测试或打包；验证范围和完成条件记录后续实施约束。用户确认方案后，切换为直接工作再实施。\n")
	}
	return b.String()
}

func CollaborationMode(p protocol.Policy) string {
	if p.CollaborationMode == "plan" {
		return "plan"
	}
	return "default"
}
func Version(p protocol.Policy) string {
	return VersionFor(p, Instructions(p))
}
func VersionFor(p protocol.Policy, instructions string) string {
	sum := sha256.Sum256(protocol.JSON(struct {
		Policy       protocol.Policy `json:"policy"`
		Instructions string          `json:"instructions"`
	}{p, instructions}))
	return hex.EncodeToString(sum[:])
}

func BaseVersion(p protocol.Policy) string {
	sum := sha256.Sum256(protocol.JSON(p.BaseInstructions))
	return hex.EncodeToString(sum[:])
}
