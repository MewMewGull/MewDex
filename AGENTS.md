# MewDex 开发约定

- 用户指出具体功能或交互问题时，先查同类 Harness 是否已有对应能力、插件或成熟做法，再结合 MewDex 的输入端、Hub 和多执行端架构实施。优先查看 DeepSeek Harness、Codex App 等产品的官方文档及相关插件源码；引用能够核实的实际行为，并说明借鉴方式与适用差异。
- 参考资料及已有决策见 [Harness 交互参考](docs/harness-references.md)。这是解决具体问题的工作方式，不要求为每个小改动开展全面竞品调研。
- 每次完成修改后重新构建并导出 Windows x64 开发测试包。使用 `npm run build:app:windows` 构建，再运行 `python scripts/export-windows-development.py`，输出固定名称 `dist/windows/MewDex-Windows-x64` 与 `dist/windows/MewDex-Windows-x64.zip`。
- 若旧版 Windows PowerShell 因模块环境找不到 `Get-FileHash`，可用 `pwsh -NoProfile -File scripts/build-desktop-development.ps1` 执行同一构建脚本；仍须取得成功构建报告后再导出。
