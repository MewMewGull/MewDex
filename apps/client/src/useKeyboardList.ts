import { useEffect, useId, useState, type KeyboardEvent } from "react";

export function useKeyboardList<T extends { id: string }>(
  items: readonly T[],
  open: boolean,
  choose: (item: T) => void,
) {
  const [selectedId, setSelectedId] = useState("");
  const listboxId = useId();
  const active = items.find((item) => item.id === selectedId) ?? items[0];
  const order = items.map((item) => item.id).join("\0");
  const optionId = (id: string) => `${listboxId}-${encodeURIComponent(id)}`;
  useEffect(() => {
    if (!open) setSelectedId("");
  }, [open]);
  useEffect(() => {
    if (!open || !active) return;
    setSelectedId(active.id);
    document
      .getElementById(optionId(active.id))
      ?.scrollIntoView({ block: "nearest" });
  }, [open, active?.id, order]);

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (
      event.nativeEvent.isComposing ||
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.shiftKey ||
      !active
    )
      return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const current = items.findIndex((item) => item.id === active.id);
      const next =
        (current + (event.key === "ArrowDown" ? 1 : -1) + items.length) %
        items.length;
      setSelectedId(items[next].id);
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(active);
    }
  }

  return { active, listboxId, optionId, setSelectedId, onKeyDown };
}
