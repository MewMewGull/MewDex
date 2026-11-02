import { test } from "node:test";
import assert from "node:assert/strict";
import { messagesFrom } from "../src/messages.ts";

test("history fragments stay distinct from a complete reply snapshot", () => {
  let sequence = 10;
  const event = (id, kind, payload) => ({
    id,
    sequence: sequence++,
    kind,
    payload,
    sessionId: "conversation",
  });
  const fragment = event("fragment", "engine.item/agentMessage/delta", {
    itemId: "reply",
    delta: "中间片段",
  });
  const complete = event("complete", "engine.item/completed", {
    item: {
      id: "reply",
      type: "agentMessage",
      text: "完整回复，包含中间片段及后文。",
    },
  });
  const firstPage = messagesFrom([fragment], "conversation");
  assert.equal(firstPage[0].partial, true);
  assert.equal(firstPage[0].text, "中间片段");
  assert.equal(firstPage[0].sequence, fragment.sequence);
  const adjacentPage = messagesFrom([fragment, complete], "conversation");
  assert.equal(adjacentPage.length, 1);
  assert.equal(adjacentPage[0].partial, false);
  assert.equal(adjacentPage[0].text, "完整回复，包含中间片段及后文。");
  assert.equal(adjacentPage[0].sequence, complete.sequence);
  const restored = messagesFrom([complete], "conversation");
  assert.equal(restored[0].id, adjacentPage[0].id);
  assert.equal(restored[0].text, adjacentPage[0].text);
  assert.deepEqual(
    messagesFrom([fragment, complete], "other-conversation"),
    [],
  );
});
