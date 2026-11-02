import { test } from "node:test";
import assert from "node:assert/strict";
import { commandID } from "../src/index.ts";

test("inputs without randomUUID still create independent IDs using secure entropy", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "crypto");
  const provider = globalThis.crypto;
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: { getRandomValues: provider.getRandomValues.bind(provider) },
  });
  try {
    const values = Array.from({ length: 128 }, commandID);
    assert.equal(new Set(values).size, values.length);
    for (const id of values)
      assert.match(
        id,
        /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      );
  } finally {
    Object.defineProperty(globalThis, "crypto", original);
  }
});

test("an input without secure entropy cannot silently generate weak command identities", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "crypto");
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: {},
  });
  try {
    assert.throws(commandID, /Secure random generation is unavailable/);
  } finally {
    Object.defineProperty(globalThis, "crypto", original);
  }
});
