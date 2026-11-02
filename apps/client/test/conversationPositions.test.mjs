import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ConversationPositions,
  captureConversationViewport,
  restoreConversationViewport,
} from "../src/conversationPositions.ts";

test("switching conversations retains history context and explicit latest navigation replaces it", () => {
  const positions = new ConversationPositions();
  const reading = {
    pinned: false,
    top: 620,
    anchor: { id: "reply-windows", sequence: 198, offset: -40 },
    expandedTools: ["windows-tool"],
  };
  positions.remember("windows-conversation", null, reading);
  positions.remember("mac-conversation", null, {
    pinned: true,
    top: 500,
    expandedTools: [],
  });
  const history = {
    direction: "before",
    cursor: 57,
    target: { id: "server-reply", version: 1 },
  };
  positions.remember("server-conversation", history, {
    ...reading,
    anchor: { id: "server-reply", sequence: 55, offset: 20 },
  });

  // The Windows cursor remains tied to what was being read even when later
  // events have moved beyond a newest-page query. Mac still follows latest.
  assert.deepEqual(positions.get("windows-conversation").history, {
    direction: "after",
    cursor: 197,
  });
  assert.equal(positions.get("windows-conversation").viewport, reading);
  assert.equal(positions.get("windows-conversation").followLatest, false);
  assert.equal(positions.get("mac-conversation").history, null);
  assert.equal(positions.get("mac-conversation").followLatest, true);
  assert.deepEqual(positions.get("server-conversation").history, history);

  positions.navigate("windows-conversation", null);
  assert.equal(positions.get("windows-conversation").viewport, undefined);
  assert.equal(positions.get("windows-conversation").followLatest, true);
  positions.navigate("server-conversation", null, false);
  assert.equal(positions.get("server-conversation").history, null);
  assert.equal(positions.get("server-conversation").followLatest, false);
  assert.equal(new ConversationPositions().get("mac-conversation"), undefined);
  positions.clear();
  for (const id of [
    "windows-conversation",
    "mac-conversation",
    "server-conversation",
  ])
    assert.equal(positions.get(id), undefined);
});

function viewport({ top = 620, anchorTop = 580, expanded = true } = {}) {
  const group = { dataset: { toolGroup: "tool" }, open: expanded };
  const otherGroup = { dataset: { toolGroup: "other" }, open: false };
  const area = {
    scrollTop: top,
    scrollHeight: 2400,
    clientHeight: 600,
    getBoundingClientRect: () => ({ top: 100, bottom: 700 }),
    querySelectorAll(selector) {
      if (selector === "details[data-tool-group][open]")
        return [group, otherGroup].filter((item) => item.open);
      if (selector === "details[data-tool-group]") return [group, otherGroup];
      return [hidden, message];
    },
  };
  const hidden = {
    dataset: { readingId: "hidden", readingSequence: "10" },
    getBoundingClientRect: () => ({ top: 0, bottom: 0, height: 0 }),
  };
  const message = {
    dataset: {
      readingId: "long-reply",
      readingSequence: "198",
      messageId: "long-reply",
    },
    closest: () => group,
    getBoundingClientRect() {
      const position = 100 + anchorTop - area.scrollTop;
      return group.open
        ? { top: position, bottom: position + 500, height: 500 }
        : { top: 0, bottom: 0, height: 0 };
    },
  };
  return { area, group, message };
}

test("the same part of a long message survives reflow and its tool expansion is restored", () => {
  const original = viewport();
  const saved = captureConversationViewport(original.area, true);
  assert.equal(saved.pinned, false);
  assert.deepEqual(saved.anchor, {
    id: "long-reply",
    sequence: 198,
    offset: -40,
  });
  const returned = viewport({ top: 0, anchorTop: 1100, expanded: false });
  returned.group.dataset.toolGroup = "later-tool";
  restoreConversationViewport(returned.area, saved);
  assert.equal(returned.group.open, true);
  assert.equal(returned.area.scrollTop, 1140);
  assert.equal(
    returned.message.getBoundingClientRect().top,
    original.message.getBoundingClientRect().top,
  );

  original.area.scrollTop = 1800;
  const following = captureConversationViewport(original.area, true);
  assert.equal(following.pinned, true);
  restoreConversationViewport(returned.area, following);
  assert.equal(returned.area.scrollTop, returned.area.scrollHeight);
  const historical = captureConversationViewport(original.area, false);
  assert.equal(historical.pinned, false);
});
