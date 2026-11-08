import { t } from "../localization.ts";
import { ExecutionTargetDetails } from "./ExecutionTargetDetails";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { FileDiff, Search, Terminal, X } from "lucide-react";
import type { CommandOutput } from "../outputs";
import { combinedCommandOutput, terminalCopyText } from "../outputs";
import { TerminalOutput } from "./TerminalOutput";
import { DiffViewer } from "./DiffViewer";
import { CopyButton } from "./CopyButton";
import {
  terminalReading,
  useOutputView,
  type OutputReading,
} from "../useOutputReading";

const commandStatus: Record<string, string> = {
  get inProgress() {
    return t("执行中");
  },
  get completed() {
    return t("已完成");
  },
  get failed() {
    return t("失败");
  },
  get declined() {
    return t("已拒绝");
  },
};

export function OutputPanel({
  view,
  loading,
  kind,
  commands,
  diff,
  focusCommand,
  historical,
  connected,
  onClose,
  onKindChange,
  onFileReference,
}: {
  view: OutputReading;
  loading: boolean;
  kind: "terminal" | "diff";
  commands: CommandOutput[];
  diff: string;
  focusCommand: { id: string; version: number } | null;
  historical: boolean;
  connected: boolean;
  onClose: () => void;
  onKindChange: (kind: "terminal" | "diff") => void;
  onFileReference?: (path: string, action?: string) => void;
}) {
  const panel = useRef<HTMLElement>(null);
  const [reading, update] = useOutputView(view);
  const { selection, findOpen } = reading;
  const [focusFind, setFocusFind] = useState(false);
  const setSelection = (selection: string) => update({ selection });
  const closeFind = useCallback(() => {
    setFocusFind(false);
    update({ findOpen: false });
  }, [update]);
  const selected =
    selection === "latest"
      ? commands.at(-1)
      : commands.find((command) => `command:${command.id}` === selection);
  const all = selection === "all";
  const missing = selection !== "latest" && !all && !selected;
  const terminalKey = all ? "all" : (selected?.id ?? selection);
  const output = useMemo(
    () => (all ? combinedCommandOutput(commands) : (selected?.output ?? "")),
    [all, commands, selected],
  );
  useEffect(() => {
    if (focusCommand && focusCommand.version !== view.focusVersion)
      update({
        selection: `command:${focusCommand.id}`,
        focusVersion: focusCommand.version,
      });
  }, [focusCommand]);
  function change(kind: "terminal" | "diff") {
    closeFind();
    onKindChange(kind);
  }
  function find() {
    setFocusFind(true);
    update({ findOpen: true });
    const field = panel.current?.querySelector<HTMLInputElement>(
      "[data-output-find-input]",
    );
    field?.focus();
    field?.select();
  }
  return (
    <section
      className="terminal-panel"
      ref={panel}
      aria-label={t("对话输出")}
      onKeyDownCapture={(event) => {
        if (
          (event.ctrlKey || event.metaKey) &&
          event.key.toLowerCase() === "f" &&
          !event.shiftKey
        ) {
          event.preventDefault();
          event.stopPropagation();
          find();
        }
      }}
    >
      <header>
        <div className="output-tabs" aria-label={t("输出内容")}>
          <button
            aria-pressed={kind === "terminal"}
            onClick={() => change("terminal")}
          >
            <Terminal size={14} />
            {t("终端")}
          </button>
          <button aria-pressed={kind === "diff"} onClick={() => change("diff")}>
            <FileDiff size={14} />
            {t("差异")}
          </button>
        </div>
        {historical && (
          <span className="output-history-label">{t("历史页")}</span>
        )}
        <button
          className="icon-button output-find-toggle"
          aria-label={t("查找当前输出")}
          title={t("查找输出 · Ctrl+F")}
          onClick={find}
        >
          <Search size={15} />
        </button>
        <button
          className="icon-button"
          aria-label={t("关闭输出面板")}
          onClick={onClose}
        >
          <X size={15} />
        </button>
      </header>
      {loading ? (
        <p className="output-empty" role="status">
          {t("正在读取当前记录的输出…")}
        </p>
      ) : kind === "terminal" ? (
        <>
          <div className="command-output-picker">
            <select
              aria-label={t("选择命令输出")}
              value={selection}
              onChange={(event) => setSelection(event.target.value)}
            >
              <option value="latest">
                {t(
                  "跟随最新命令{0}",
                  commands.length ? t(" · {0} 条", commands.length) : "",
                )}
              </option>
              <option value="all">{t("全部命令输出")}</option>
              {missing && (
                <option value={selection}>{t("所选命令不在当前记录")}</option>
              )}
              {commands.map((command) => (
                <option key={command.id} value={`command:${command.id}`}>
                  {command.command || t("历史输出片段")}
                </option>
              ))}
            </select>
            {!missing && commands.length > 0 && (
              <CopyButton
                text={terminalCopyText(output)}
                label={t("复制输出")}
              />
            )}
          </div>
          {selected && !all && (
            <div className="command-output-heading">
              <ExecutionTargetDetails target={selected.executionTarget} />
              {selected.command && (
                <div>
                  <code>{selected.command}</code>
                  <CopyButton text={selected.command} label={t("复制命令")} />
                </div>
              )}
              <p>
                <span
                  className={
                    selected.status === "failed" ? "command-failed" : ""
                  }
                >
                  {selected.status === "inProgress" &&
                  (historical || !connected)
                    ? t("上次执行中")
                    : (commandStatus[selected.status] ?? t("输出记录"))}
                </span>
                {selected.exitCode !== undefined && (
                  <span>{t("退出码 {0}", selected.exitCode)}</span>
                )}
                {selected.durationMs !== undefined && (
                  <span>
                    {selected.durationMs < 1000
                      ? `${selected.durationMs} ms`
                      : `${(selected.durationMs / 1000).toFixed(1)} s`}
                  </span>
                )}
                {selected.partial && <span>{t("当前记录仅含部分输出")}</span>}
              </p>
              {selected.cwd && (
                <small title={selected.cwd}>{selected.cwd}</small>
              )}
            </div>
          )}
          {missing ? (
            <div className="output-empty" role="status">
              {t("所选命令不在当前记录中，原选择仍然保留。")}
              <button onClick={() => setSelection("latest")}>
                {t("查看当前记录的最新命令")}
              </button>
            </div>
          ) : !commands.length ? (
            <p className="output-empty">{t("当前记录还没有命令输出。")}</p>
          ) : (
            <TerminalOutput
              key={terminalKey}
              view={terminalReading(view, terminalKey)}
              output={output}
              findOpen={findOpen}
              autoFocusFind={focusFind}
              onCloseFind={closeFind}
              onReadingEarlier={() => {
                if (selection === "latest" && selected)
                  setSelection(`command:${selected.id}`);
              }}
            />
          )}
        </>
      ) : (
        <DiffViewer
          view={view.diff}
          output={diff}
          findOpen={findOpen}
          autoFocusFind={focusFind}
          onCloseFind={closeFind}
          onFileReference={onFileReference}
        />
      )}
    </section>
  );
}
