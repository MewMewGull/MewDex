import { test } from "node:test";
import assert from "node:assert/strict";
import {
  completeSidebarOrder,
  emptySidebarLayout,
  moveSidebarItem,
  readSidebarLayout,
} from "../src/sidebarLayout.ts";

test("saved sidebar ordering preserves filtered and temporarily missing rows", () => {
  const complete = completeSidebarOrder(
    ["hidden", "b", "a"],
    ["a", "b", "new"],
  );
  assert.deepEqual(moveSidebarItem(complete, "a", "b"), [
    "hidden",
    "a",
    "b",
    "new",
  ]);
  assert.deepEqual(moveSidebarItem(complete, "b", "new", true), [
    "hidden",
    "a",
    "new",
    "b",
  ]);
  assert.deepEqual(moveSidebarItem(complete, "a", "a"), complete);
  assert.deepEqual(completeSidebarOrder([""], ["", "SOTT"]), ["", "SOTT"]);
});

test("old and malformed workspace settings receive safe sidebar defaults", () => {
  assert.deepEqual(readSidebarLayout(undefined), emptySidebarLayout());
  assert.deepEqual(
    readSidebarLayout({ sections: ["SOTT", "SOTT", 3, ""], projects: "bad" }),
    {
      ...emptySidebarLayout(),
      sections: ["SOTT", ""],
    },
  );
});
