import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeTextPreview } from "../src/textPreview.ts";

test("text preview reads UTF BOMs and allows explicit Windows legacy decoding without changing bytes", () => {
  assert.deepEqual(
    decodeTextPreview(new Uint8Array([0xef, 0xbb, 0xbf, 0x41]), "auto", false),
    {
      encoding: "utf-8",
      text: "A",
    },
  );
  assert.deepEqual(
    decodeTextPreview(
      new Uint8Array([0xff, 0xfe, 0x41, 0x00, 0x2d, 0x4e]),
      "auto",
      false,
    ),
    {
      encoding: "utf-16le",
      text: "A中",
    },
  );
  assert.deepEqual(
    decodeTextPreview(
      new Uint8Array([0xfe, 0xff, 0x00, 0x41, 0x4e, 0x2d]),
      "auto",
      false,
    ),
    {
      encoding: "utf-16be",
      text: "A中",
    },
  );
  assert.equal(
    decodeTextPreview(new TextEncoder().encode("中文\r\n内容"), "auto", false)
      .text,
    "中文\r\n内容",
  );
  const original = new Uint8Array([0xd6, 0xd0, 0xce, 0xc4]);
  assert.equal(decodeTextPreview(original, "gb18030", false).text, "中文");
  assert.notEqual(decodeTextPreview(original, "utf-8", false).text, "中文");
  assert.deepEqual([...original], [0xd6, 0xd0, 0xce, 0xc4]);
  assert.equal(decodeTextPreview(new Uint8Array(), "auto", false).text, "");
});

test("a bounded preview omits an incomplete final character while a complete file reports invalid encoding normally", () => {
  const utf8 = new Uint8Array([0x41, 0xe4, 0xb8]);
  assert.equal(decodeTextPreview(utf8, "auto", true).text, "A");
  assert.equal(decodeTextPreview(utf8, "auto", false).text, "A\ufffd");
  const utf16 = new Uint8Array([0xff, 0xfe, 0x41, 0, 0x3d, 0xd8]);
  assert.equal(decodeTextPreview(utf16, "auto", true).text, "A");
  assert.equal(decodeTextPreview(utf16, "auto", false).text, "A\ufffd");
  assert.equal(
    decodeTextPreview(new Uint8Array([0x41, 0xd6]), "gb18030", true).text,
    "A",
  );
});
