import { useEffect, useRef, useState } from "react";
import { SettingAutosave } from "./settingAutosave";

export function useSettingAutosave<T>(
  initial: T,
  save: (value: T) => Promise<unknown>,
  onPending?: (pending: boolean) => void,
) {
  const saveRef = useRef(save);
  saveRef.current = save;
  const pendingRef = useRef(onPending);
  pendingRef.current = onPending;
  const [queue] = useState(
    () => new SettingAutosave(initial, (value) => saveRef.current(value)),
  );
  const [state, setState] = useState(queue.state);
  useEffect(() => {
    const unsubscribe = queue.subscribe((next) => {
      setState(next);
      pendingRef.current?.(next.pending);
    });
    return () => {
      unsubscribe();
      void queue.flush();
      pendingRef.current?.(false);
    };
  }, [queue]);
  return { queue, state };
}
