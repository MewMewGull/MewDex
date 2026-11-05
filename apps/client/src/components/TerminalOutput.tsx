import { t } from "../localization.ts";
import { useEffect, useRef, useState } from "react";
import { Terminal as XTerminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon, type ISearchOptions } from "@xterm/addon-search";
import { ArrowDown } from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import { CopyButton } from "./CopyButton";
import { OutputFindBar } from "./OutputFindBar";
import { useOutputView, type TerminalReading } from "../useOutputReading";

function terminalColors(host: HTMLElement) {
  const styles = getComputedStyle(host);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const context = canvas.getContext("2d")!;
  const backgrounds: string[] = [];
  for (
    let element: HTMLElement | null = host;
    element;
    element = element.parentElement
  )
    backgrounds.unshift(getComputedStyle(element).backgroundColor);
  const canvasBackground =
    getComputedStyle(document.documentElement).colorScheme === "light"
      ? "#ffffff"
      : "#10131b";
  // The search addon requires hex colors, including for CSS color-mix tokens.
  function color(property: string) {
    // Composite alpha colors over the same surfaces as the visible terminal.
    context.fillStyle = canvasBackground;
    context.fillRect(0, 0, 1, 1);
    for (const background of backgrounds) {
      context.fillStyle = background;
      context.fillRect(0, 0, 1, 1);
    }
    if (property !== "background-color") {
      context.fillStyle = styles.getPropertyValue(property).trim();
      context.fillRect(0, 0, 1, 1);
    }
    const channels = context.getImageData(0, 0, 1, 1).data;
    return `#${Array.from(channels.slice(0, 3), (channel) => channel.toString(16).padStart(2, "0")).join("")}`;
  }
  const accent = color("--accent-text");
  const border = color("--accent-border");
  const selection = color("--accent-selection");
  return {
    theme: {
      background: color("background-color"),
      foreground: color("color"),
      red: color("--status-error-text"),
      green: color("--status-success-text"),
      yellow: color("--status-warning-text"),
      blue: color("--status-info-text"),
      cyan: accent,
      brightRed: color("--status-error-text"),
      brightGreen: color("--status-success-text"),
      brightYellow: color("--status-warning-text"),
      brightBlue: color("--status-info-text"),
      brightCyan: accent,
      selectionBackground: selection,
      selectionInactiveBackground: selection,
    },
    decorations: {
      matchBackground: color("--accent-surface"),
      matchBorder: border,
      matchOverviewRuler: border,
      activeMatchBackground: selection,
      activeMatchBorder: accent,
      activeMatchColorOverviewRuler: accent,
    },
  };
}

export function TerminalOutput({
  view,
  output,
  findOpen,
  autoFocusFind,
  onCloseFind,
  onReadingEarlier,
}: {
  view: TerminalReading;
  output: string;
  findOpen: boolean;
  autoFocusFind: boolean;
  onCloseFind: () => void;
  onReadingEarlier: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<XTerminal | null>(null);
  const search = useRef<SearchAddon | null>(null);
  const decorations = useRef<ISearchOptions["decorations"]>(undefined);
  const previous = useRef("");
  const reading = useRef(onReadingEarlier);
  reading.current = onReadingEarlier;
  const [saved, update] = useOutputView(view);
  const query = saved.query;
  const initialViewport = useRef(view.viewport);
  const restored = useRef(false);
  const searchRequest = useRef({ query, findOpen });
  searchRequest.current = { query, findOpen };
  const [matches, setMatches] = useState({ resultIndex: -1, resultCount: 0 });
  const [selected, setSelected] = useState(false);
  const [following, setFollowing] = useState(view.viewport?.pinned ?? true);
  useEffect(() => {
    const colors = terminalColors(host.current!);
    decorations.current = colors.decorations;
    const term = new XTerminal({
      convertEol: true,
      disableStdin: true,
      allowProposedApi: true,
      fontSize: 11,
      fontFamily: "Consolas, monospace",
      scrollback: 5000,
      theme: colors.theme,
    });
    const fit = new FitAddon();
    const finder = new SearchAddon();
    term.loadAddon(fit);
    term.loadAddon(finder);
    term.open(host.current!);
    terminal.current = term;
    search.current = finder;
    previous.current = "";
    restored.current = false;
    const resize = () => {
      if (host.current!.clientWidth > 0 && host.current!.clientHeight > 0)
        fit.fit();
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(host.current!);
    const scrolling = term.onScroll(() => {
      if (!restored.current) return;
      const pinned = term.buffer.active.viewportY === term.buffer.active.baseY;
      view.viewport = { top: term.buffer.active.viewportY, pinned };
      setFollowing(pinned);
      if (!pinned) reading.current();
    });
    const selection = term.onSelectionChange(() =>
      setSelected(term.hasSelection()),
    );
    const results = finder.onDidChangeResults(setMatches);
    const updateTheme = () => {
      const colors = terminalColors(host.current!);
      term.options.theme = colors.theme;
      decorations.current = colors.decorations;
      const request = searchRequest.current;
      if (restored.current && request.findOpen && request.query) {
        finder.clearDecorations();
        finder.findNext(request.query, {
          decorations: decorations.current,
          incremental: true,
        });
      }
    };
    const appearance = new MutationObserver(updateTheme);
    appearance.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["style", "data-theme"],
    });
    const systemTheme = matchMedia("(prefers-color-scheme: light)");
    systemTheme.addEventListener("change", updateTheme);
    return () => {
      if (restored.current)
        view.viewport = {
          top: term.buffer.active.viewportY,
          pinned: term.buffer.active.viewportY === term.buffer.active.baseY,
        };
      observer.disconnect();
      appearance.disconnect();
      systemTheme.removeEventListener("change", updateTheme);
      scrolling.dispose();
      selection.dispose();
      results.dispose();
      term.dispose();
      terminal.current = null;
      search.current = null;
    };
  }, []);
  useEffect(() => {
    const term = terminal.current;
    if (!term) return;
    const append = output.startsWith(previous.current);
    const viewport = term.buffer.active.viewportY;
    const wasPinned = viewport === term.buffer.active.baseY;
    if (!append) term.reset();
    term.write(append ? output.slice(previous.current.length) : output, () => {
      if (terminal.current !== term) return;
      if (!restored.current) {
        const request = searchRequest.current;
        if (request.findOpen && request.query)
          search.current?.findNext(request.query, {
            decorations: decorations.current,
            incremental: true,
          });
        const savedViewport = initialViewport.current;
        if (savedViewport && !savedViewport.pinned)
          term.scrollToLine(savedViewport.top);
        else term.scrollToBottom();
        restored.current = true;
      } else if (!append && !wasPinned) term.scrollToLine(viewport);
      const pinned = term.buffer.active.viewportY === term.buffer.active.baseY;
      view.viewport = { top: term.buffer.active.viewportY, pinned };
      setFollowing(pinned);
    });
    previous.current = output;
  }, [output]);
  useEffect(() => {
    if (!findOpen || !query) {
      search.current?.clearDecorations();
      setMatches({ resultIndex: -1, resultCount: 0 });
    } else if (restored.current)
      search.current?.findNext(query, {
        decorations: decorations.current,
        incremental: true,
      });
  }, [query, findOpen]);
  function next(previous = false) {
    if (!query) return;
    const finder = search.current;
    if (previous)
      finder?.findPrevious(query, { decorations: decorations.current });
    else finder?.findNext(query, { decorations: decorations.current });
  }
  return (
    <div className="terminal-view">
      {findOpen && (
        <OutputFindBar
          query={query}
          onQuery={(query) => update({ query })}
          autoFocus={autoFocusFind}
          label={t("查找当前终端缓冲区")}
          status={
            !query
              ? ""
              : matches.resultCount >= 1000
                ? t("1000+ 项")
                : matches.resultCount
                  ? `${matches.resultIndex >= 0 ? matches.resultIndex + 1 + "/" : ""}${matches.resultCount}`
                  : t("无匹配")
          }
          hasResults={matches.resultCount > 0}
          onPrevious={() => next(true)}
          onNext={() => next()}
          onClose={onCloseFind}
        />
      )}
      <div className="xterm-host" ref={host} />
      <div className="terminal-view-footer">
        {selected && (
          <CopyButton
            text={() => terminal.current?.getSelection() ?? ""}
            label={t("复制选中")}
          />
        )}
        <span>{t("5000 行滚动记录")}</span>
        {!following && (
          <button
            onClick={() => {
              terminal.current?.scrollToBottom();
              setFollowing(true);
            }}
          >
            <ArrowDown size={13} />
            {t(" 跟随最新")}
          </button>
        )}
      </div>
    </div>
  );
}
