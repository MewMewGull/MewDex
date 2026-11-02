import type { Policy } from "@mewdex/sdk";

export const defaultPolicy = (): Policy => ({
  mode: "focused",
  noTests: false,
  instructions: "",
  completion: "",
});

export const samePolicy = (a: Policy, b: Policy): boolean =>
  JSON.stringify([
    a.mode,
    a.noTests,
    a.instructions,
    a.completion,
    a.model || "",
    a.reasoningEffort || "",
    a.collaborationMode || "",
    a.accessMode || "",
    a.baseInstructions ?? null,
  ]) ===
  JSON.stringify([
    b.mode,
    b.noTests,
    b.instructions,
    b.completion,
    b.model || "",
    b.reasoningEffort || "",
    b.collaborationMode || "",
    b.accessMode || "",
    b.baseInstructions ?? null,
  ]);
