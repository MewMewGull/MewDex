import { useCallback, useRef, useState } from "react";

export interface TerminalReading {
  query: string;
  viewport?: { top: number; pinned: boolean };
}

export interface DiffReading {
  selection: string | null;
  raw: boolean;
  wrap: boolean;
  query: string;
  index: number;
  top: number;
  left: number;
}

export interface OutputReading {
  selection: string;
  findOpen: boolean;
  focusVersion: number;
  terminals: Map<string, TerminalReading>;
  diff: DiffReading;
}

// Window-local reading choices only. No output content or persisted state.
export function useOutputReading() {
  const views = useRef(new Map<string, OutputReading>());
  return {
    get(key: string) {
      let view = views.current.get(key);
      if (!view) {
        view = {
          selection: "latest",
          findOpen: false,
          focusVersion: 0,
          terminals: new Map(),
          diff: {
            selection: null,
            raw: false,
            wrap: false,
            query: "",
            index: 0,
            top: 0,
            left: 0,
          },
        };
        views.current.set(key, view);
      }
      return view;
    },
    clear() {
      views.current.clear();
    },
  };
}

export function terminalReading(view: OutputReading, key: string) {
  let saved = view.terminals.get(key);
  if (!saved) {
    saved = { query: "" };
    view.terminals.set(key, saved);
  }
  return saved;
}

export function useOutputView<T extends object>(saved: T) {
  const [value, setValue] = useState(() => ({ ...saved }));
  const update = useCallback(
    (patch: Partial<T>) => {
      Object.assign(saved, patch);
      setValue({ ...saved });
    },
    [saved],
  );
  return [value, update] as const;
}
