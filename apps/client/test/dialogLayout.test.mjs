import { test } from "node:test";
import assert from "node:assert/strict";
import {
  fitDialog,
  parseDialogLayout,
  resizeDialog,
} from "../src/dialogLayout.ts";

test("settings dialogs open larger and remain within desktop and mobile viewports", () => {
  assert.deepEqual(fitDialog({}, { width: 1920, height: 1080 }), {
    width: 1400,
    height: 900,
    left: 260,
    top: 90,
  });
  for (const viewport of [
    { width: 1280, height: 720 },
    { width: 390, height: 844 },
    { width: 320, height: 240 },
    { width: 0, height: 0 },
  ]) {
    for (const layout of [
      {},
      { size: { width: 3000, height: 2000 } },
      { maximized: true },
    ]) {
      const rect = fitDialog(layout, viewport, { left: 2000, top: 1500 });
      assert.ok(rect.left >= 0 && rect.top >= 0);
      assert.ok(rect.width >= 0 && rect.height >= 0);
      assert.ok(rect.left + rect.width <= viewport.width);
      assert.ok(rect.top + rect.height <= viewport.height);
    }
  }
});

test("all resize edges keep the opposite edge stationary and enforce minimum sizes", () => {
  const viewport = { width: 1920, height: 1080 };
  const start = { width: 1000, height: 700, left: 400, top: 180 };
  assert.deepEqual(resizeDialog(start, "nw", { x: -100, y: -50 }, viewport), {
    width: 1100,
    height: 750,
    left: 300,
    top: 130,
  });
  assert.deepEqual(
    resizeDialog(start, "se", { x: -2000, y: -2000 }, viewport),
    {
      width: 560,
      height: 360,
      left: 400,
      top: 180,
    },
  );
  for (const direction of ["n", "s", "e", "w", "ne", "nw", "se", "sw"]) {
    const resized = resizeDialog(
      start,
      direction,
      { x: 5000, y: 5000 },
      viewport,
    );
    assert.ok(resized.width >= 560 && resized.height >= 360);
    assert.ok(resized.left >= 24 && resized.top >= 24);
    assert.ok(resized.left + resized.width <= viewport.width - 24);
    assert.ok(resized.top + resized.height <= viewport.height - 24);
    if (!direction.includes("w")) assert.equal(resized.left, start.left);
    if (!direction.includes("e"))
      assert.equal(resized.left + resized.width, start.left + start.width);
    if (!direction.includes("n")) assert.equal(resized.top, start.top);
    if (!direction.includes("s"))
      assert.equal(resized.top + resized.height, start.top + start.height);
  }
});

test("resizing handles a viewport change during a drag", () => {
  const resized = resizeDialog(
    { width: 1400, height: 900, left: 260, top: 90 },
    "nw",
    { x: 1000, y: 1000 },
    { width: 390, height: 600 },
  );
  assert.deepEqual(resized, { width: 370, height: 360, left: 10, top: 230 });
});

test("layout persistence validates old, corrupt and unavailable values", () => {
  for (const raw of [
    null,
    "bad JSON",
    "null",
    "[]",
    '{"size":{"width":-10,"height":700}}',
    '{"size":{"width":"900","height":700}}',
    '{"size":{"width":1e999,"height":700}}',
  ]) {
    assert.equal(parseDialogLayout(raw).size, undefined);
    assert.ok(
      Number.isFinite(
        fitDialog(parseDialogLayout(raw), { width: 1920, height: 1080 }).width,
      ),
    );
  }
  const layout = { size: { width: 1100, height: 740 }, maximized: true };
  assert.deepEqual(parseDialogLayout(JSON.stringify(layout)), layout);
  assert.equal(parseDialogLayout('{"maximized":"true"}').maximized, false);
  const saved = { size: { width: 1400, height: 900 } };
  fitDialog(saved, { width: 390, height: 600 });
  assert.deepEqual(saved, { size: { width: 1400, height: 900 } });
});
