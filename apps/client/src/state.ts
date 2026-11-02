import { create } from "zustand";
import type { Event, Policy } from "@mewdex/sdk";
import { EventWindow } from "@mewdex/sdk";
import { defaultPolicy } from "./policyValues.ts";

const timeline = new EventWindow();
const drafts = new Map<
  string,
  { policy: Policy; policyBasis: string; policyDirty: boolean }
>();

export interface WorkspaceSelection {
  deviceId: string;
  sessionId: string;
  selectionVersion: number;
}

export const selectionOf = (s: WorkspaceSelection): WorkspaceSelection => ({
  deviceId: s.deviceId,
  sessionId: s.sessionId,
  selectionVersion: s.selectionVersion,
});

export const sameSelection = (
  a: WorkspaceSelection,
  b: WorkspaceSelection,
): boolean =>
  a.deviceId === b.deviceId &&
  a.sessionId === b.sessionId &&
  a.selectionVersion === b.selectionVersion;

interface WorkspaceState extends WorkspaceSelection {
  events: Event[];
  eventsTruncated: boolean;
  following: boolean;
  connectionState: string;
  policy: Policy;
  policyBasis: string;
  policyDirty: boolean;
  selectDevice(id: string): void;
  selectSession(id: string): void;
  appendEvent(event: Event): void;
  loadEvents(sessionId: string, events: Event[], following: boolean): void;
  followEvents(following: boolean): void;
  clearEvents(): void;
  setConnectionState(value: string): void;
  setPolicy(value: Partial<Policy>): void;
  loadPolicy(value: Policy, version: string): void;
  reset(): void;
}

function rememberPolicy(state: WorkspaceState) {
  if (!state.sessionId || !state.policyDirty) return;
  drafts.delete(state.sessionId);
  drafts.set(state.sessionId, {
    policy: state.policy,
    policyBasis: state.policyBasis,
    policyDirty: true,
  });
  while (drafts.size > 16) drafts.delete(drafts.keys().next().value!);
}

export const useWorkspace = create<WorkspaceState>((set) => ({
  deviceId: "",
  sessionId: "",
  selectionVersion: 0,
  events: [],
  eventsTruncated: false,
  following: true,
  connectionState: "connecting",
  policy: defaultPolicy(),
  policyBasis: "",
  policyDirty: false,
  selectDevice: (deviceId) =>
    set((s) => {
      rememberPolicy(s);
      timeline.clear();
      return {
        deviceId,
        sessionId: "",
        selectionVersion: s.selectionVersion + 1,
        policy: defaultPolicy(),
        policyBasis: "",
        policyDirty: false,
        events: [],
        eventsTruncated: false,
        following: true,
      };
    }),
  selectSession: (sessionId) =>
    set((s) => {
      if (s.sessionId === sessionId)
        return { selectionVersion: s.selectionVersion + 1 };
      rememberPolicy(s);
      timeline.clear();
      return {
        events: [],
        eventsTruncated: false,
        following: true,
        sessionId,
        selectionVersion: s.selectionVersion + 1,
        ...(drafts.get(sessionId) ?? {
          policy: defaultPolicy(),
          policyBasis: "",
          policyDirty: false,
        }),
      };
    }),
  appendEvent: (event) =>
    set((s) => {
      if (
        !s.following ||
        !s.sessionId ||
        event.sessionId !== s.sessionId ||
        !timeline.append(event)
      )
        return s;
      return { events: timeline.events, eventsTruncated: timeline.truncated };
    }),
  loadEvents: (sessionId, events, following) =>
    set((s) => {
      if (s.sessionId !== sessionId) return s;
      if (!following || !s.following) timeline.clear();
      timeline.merge(events);
      return {
        events: timeline.events,
        eventsTruncated: timeline.truncated,
        following,
      };
    }),
  followEvents: (following) =>
    set((s) => {
      if (s.following === following) return s;
      timeline.clear();
      return { following, events: [], eventsTruncated: false };
    }),
  clearEvents: () =>
    set(() => {
      timeline.clear();
      return { events: [], eventsTruncated: false };
    }),
  setConnectionState: (connectionState) => set({ connectionState }),
  setPolicy: (policy) =>
    set((s) => ({ policy: { ...s.policy, ...policy }, policyDirty: true })),
  loadPolicy: (policy, policyBasis) =>
    set((s) => {
      drafts.delete(s.sessionId);
      return { policy, policyBasis, policyDirty: false };
    }),
  reset: () =>
    set((s) => {
      timeline.clear();
      drafts.clear();
      return {
        deviceId: "",
        sessionId: "",
        selectionVersion: s.selectionVersion + 1,
        events: [],
        eventsTruncated: false,
        following: true,
        connectionState: "closed",
        policy: defaultPolicy(),
        policyBasis: "",
        policyDirty: false,
      };
    }),
}));
