import { test } from "node:test";
import assert from "node:assert/strict";
import { useWorkspace, selectionOf, sameSelection } from "../src/state.ts";

test("unfinished task policies stay with their conversation and clear with private state", () => {
  const state = () => useWorkspace.getState();
  state().reset();
  state().selectSession("first");
  state().setPolicy({ noTests: true, instructions: "first task only" });
  state().selectSession("second");
  assert.equal(state().policy.noTests, false);
  assert.equal(state().policyDirty, false);
  state().selectSession("first");
  assert.equal(state().policy.instructions, "first task only");
  state().reset();
  state().selectSession("first");
  assert.equal(state().policy.instructions, "");
  state().reset();
});

test("history browsing keeps live output separate and rejects another conversation's late page", () => {
  const state = () => useWorkspace.getState();
  const event = (sequence, sessionId = "s") => ({
    id: String(sequence),
    sequence,
    sessionId,
    kind: "message.user",
    payload: { text: "x" },
    deviceId: "node",
    createdAt: "",
  });
  state().reset();
  state().selectSession("s");
  state().appendEvent(event(100));
  state().loadEvents("s", [event(99), event(100)], true);
  assert.deepEqual(
    state().events.map((e) => e.sequence),
    [99, 100],
  );
  state().followEvents(false);
  state().loadEvents("s", [event(10)], false);
  state().appendEvent(event(101));
  assert.deepEqual(
    state().events.map((e) => e.sequence),
    [10],
  );
  state().followEvents(true);
  state().loadEvents("s", [event(100), event(101)], true);
  state().selectSession("other");
  state().loadEvents("s", [event(102)], true);
  assert.deepEqual(state().events, []);
  state().reset();
});

test("late creation and send results cannot claim a new selection with the same IDs", () => {
  const state = () => useWorkspace.getState();
  state().reset();
  state().selectDevice("computer");
  const creating = selectionOf(state());
  state().selectSession(""); // Explicitly starting another new conversation.
  assert.equal(sameSelection(creating, state()), false);
  state().selectSession("first");
  const sending = selectionOf(state());
  state().selectSession("second");
  assert.equal(sameSelection(sending, state()), false);
  state().selectSession("first");
  assert.equal(sameSelection(sending, state()), false);
  const beforeReset = selectionOf(state());
  state().reset();
  state().selectDevice("computer");
  state().selectSession("first");
  assert.equal(sameSelection(beforeReset, state()), false);
  state().reset();
});
