import { useEffect, useRef, useState } from "react";
import { Terminal as XTerminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { ArrowDown } from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import { CopyButton } from "./CopyButton";
import { OutputFindBar } from "./OutputFindBar";
import { useOutputView, type TerminalReading } from "../useOutputReading";

const decorations = {
  matchBackground: "#3e5237",
  matchBorder: "#61734f",
  matchOverviewRuler: "#61734f",
  activeMatchBackground: "#697f45",
  activeMatchBorder: "#b3cb8a",
  activeMatchColorOverviewRuler: "#b3cb8a",
};

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
    const term = new XTerminal({
      convertEol: true,
      disableStdin: true,
      allowProposedApi: true,
      fontSize: 11,
      fontFamily: "Consolas, monospace",
      scrollback: 5000,
      theme: {
        background: "#0d1119",
        foreground: "#a6b7cc",
        selectionBackground: "#405b53",
      },
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
    return () => {
      if (restored.current)
        view.viewport = {
          top: term.buffer.active.viewportY,
          pinned: term.buffer.active.viewportY === term.buffer.active.baseY,
        };
      observer.disconnect();
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
            decorations,
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
      search.current?.findNext(query, { decorations, incremental: true });
  }, [query, findOpen]);
  function next(previous = false) {
    if (!query) return;
    const finder = search.current;
    if (previous) finder?.findPrevious(query, { decorations });
    else finder?.findNext(query, { decorations });
  }
  return (
    <div className="terminal-view">
      {findOpen && (
        <OutputFindBar
          query={query}
          onQuery={(query) => update({ query })}
          autoFocus={autoFocusFind}
          label="查找当前终端缓冲区"
          status={
            !query
              ? ""
              : matches.resultCount >= 1000
                ? "1000+ 项"
                : matches.resultCount
                  ? `${matches.resultIndex >= 0 ? matches.resultIndex + 1 + "/" : ""}${matches.resultCount}`
                  : "无匹配"
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
            label="复制选中"
          />
        )}
        <span>5000 行滚动记录</span>
        {!following && (
          <button
            onClick={() => {
              terminal.current?.scrollToBottom();
              setFollowing(true);
            }}
          >
            <ArrowDown size={13} /> 跟随最新
          </button>
        )}
      </div>
    </div>
  );
}
