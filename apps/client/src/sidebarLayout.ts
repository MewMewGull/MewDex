export interface SidebarLayout {
  sections: string[];
  projects: string[];
  collapsedSections: string[];
  collapsedProjects: string[];
}

export const emptySidebarLayout = (): SidebarLayout => ({
  sections: [],
  projects: [],
  collapsedSections: [],
  collapsedProjects: [],
});

export function readSidebarLayout(value: unknown): SidebarLayout {
  const layout = emptySidebarLayout();
  if (!value || typeof value !== "object") return layout;
  for (const key of Object.keys(layout) as (keyof SidebarLayout)[]) {
    const items = (value as Partial<SidebarLayout>)[key];
    if (Array.isArray(items))
      layout[key] = [
        ...new Set(items.filter((item) => typeof item === "string")),
      ];
  }
  return layout;
}

// Always reorder the complete sequence, including rows hidden by a filter.
export function completeSidebarOrder(saved: string[], current: string[]) {
  return [...new Set([...saved, ...current])];
}

export function moveSidebarItem(
  order: string[],
  source: string,
  target: string,
  after = false,
) {
  if (source === target || !order.includes(source) || !order.includes(target))
    return order;
  const next = order.filter((item) => item !== source);
  next.splice(next.indexOf(target) + Number(after), 0, source);
  return next;
}
