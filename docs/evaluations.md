# Hub 任务评测

Hub 发布包的 `mewdex-hub evaluate` 命令通过正常输入接口创建对话、启动模型回合、读取历史并核对结果。开发检出可使用 `go run ./cmd/mewdex evaluate`。评测支持多个明确指定的模型及推理强度。

## 准备任务

复制 [示例任务集](evaluations/core.example.json)，填写当前 Hub 的项目编号和模型标识。`format` 固定为 `1`。同一任务集支持 1 至 4 组模型设置及 1 至 20 个任务，每个任务包含稳定编号、用户输入、任务策略与检查项。

任务默认使用只读权限。需要工作区写入或网络的任务须在 `policy.accessMode` 中明确设置。运行前在项目目录组中核对机器和路径，评测会保存项目目录顺序及项目配置版本。

检查项支持下列类型。

| kind | 核对内容 | 必填字段 |
|---|---|---|
| `assistant.equals` | 完成的助手消息与预期文本完全相等 | `value` |
| `assistant.contains` | 完成的助手消息包含预期文本 | `value` |
| `tool.count` | 工具历史条目数量 | `toolType`、`count` |
| `command` | 目标机器、目录、原始参数数组、权限、退出码及输出 | `deviceId`、`projectId`、`command`、`count` |
| `computer` | 指定机器的窗口工具操作已完成 | `deviceId`、`projectId`、`operation`、`count` |

`toolType` 支持 `commandExecution`、`fileChange`、`mcpToolCall` 和 `computerUse`。命令检查可填写 `outputContains`，默认要求成功完成及退出码为零。预期失败的命令可设置 `status` 为 `failed` 并填写明确的 `exitCode`。

需要逐步执行的任务可设置 `sequentialCommands: true`。评测按 `command` 检查项的排列顺序展开各项 `count`，核对 `mewdex_exec` 的实际启动顺序，并通过返回的持久任务编号关联执行结果。每一步必须在前一步调用返回且任务终态已确认后开始。异步命令可以通过 `mewdex_wait` 等待完成。多余调用、错序、执行重叠、缺失回执和起止参数变化均会导致检查失败。配置示例见 [顺序命令任务](evaluations/ordered.example.json)。

此字段默认关闭，已有任务集可以继续使用原有检查规则和恢复目录。

评测同时核对实际模型、回合配置快照、目录绑定、对话结束状态及工具结果是否确认。窗口控制沿用执行端本机授权流程。

## 运行与恢复

输入连接沿用 APP 使用的 JSON 结构。通过标准输入或 `-connection` 文件传入。

```json
{
  "baseURL": "https://hub.example.com",
  "hubDeviceId": "your-hub-device-id",
  "credentials": {
    "token": "YOUR_INPUT_TOKEN",
    "identity": { "id": "your-input-identity-id" }
  }
}
```

```sh
./mewdex-hub evaluate -suite core.json -connection input-connection.json -output .state/evaluations/run-001
```

同一个输出目录保存一轮运行。再次执行相同命令会查询原始指令回执并继续观察原对话。提交后响应丢失时，运行记录保留原指令编号。找不到原回执或执行结果未知时，需要先在输入端核对该对话。`-timeout 5m` 控制每项任务的观察时长，到期会保留当前运行状态。

任务集、Hub 身份或输入身份变化后，为新一轮评测使用新的输出目录。评测完成的自建对话会归档，证据保留在原对话及输出目录。

## 审阅与改进

输出目录包含以下文件。

- `report.json` 保存任务集哈希、模型矩阵、运行时版本、配置快照和逐项检查结果。
- `review.md` 展示每项任务的预期与实际结果，并链接完整证据。
- `*.trace.json` 保存模型提供方、任务策略、回合配置和分页读取的事件记录。
- `run.db` 保存恢复所需的内部指令与阶段状态。

查看失败检查对应的原始工具参数、执行机器和配置版本，在项目配置编辑器中审阅具体改动。随后用同一任务集在新目录运行，比较改动前后的结果。报告及证据会包含任务输入、输出与项目路径，适合保存在项目内部的评测目录。

当前运行时适配基准为固定版 Codex app-server。模型目录提供元数据，成功完成的真实回合提供该次模型调用的可用性证据。第三方模型提供方与其他 Agent 运行时需要各自的真实评测记录。

任务、运行证据与确定性检查的组织方式参考 OpenAI Docs。[任务评测示例](https://developers.openai.com/blog/eval-skills)
