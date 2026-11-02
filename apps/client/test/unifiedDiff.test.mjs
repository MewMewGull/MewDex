import { test } from "node:test";
import assert from "node:assert/strict";
import { parseUnifiedDiff } from "../src/unifiedDiff.ts";

test("unified files retain line coordinates, Git quoted Unicode paths and exact raw patches", () => {
  const original = String.raw`diff --git a/src/a.ts b/src/a.ts
index 123..456 100644
--- a/src/a.ts
+++ b/src/a.ts
@@ -2,3 +2,4 @@ function run
 keep
-old
+new
+extra
 end
\ No newline at end of file
diff --git "a/\346\265\213\350\257\225 name.txt" "b/\346\265\213\350\257\225 name.txt"
new file mode 100644
--- /dev/null
+++ "b/\346\265\213\350\257\225 name.txt"
@@ -0,0 +1 @@
+hello
diff --git a/removed.txt b/removed.txt
deleted file mode 100644
--- a/removed.txt
+++ /dev/null
@@ -1 +0,0 @@
-deleted
diff --git a/old name.txt b/new name.txt
similarity index 100%
rename from old name.txt
rename to new name.txt
diff --git a/picture.bin b/picture.bin
Binary files a/picture.bin and b/picture.bin differ
`;
  const files = parseUnifiedDiff(original);
  assert.equal(files.length, 5);
  assert.deepEqual([files[0].added, files[0].removed], [2, 1]);
  assert.deepEqual(
    files[0].hunks[0].lines.map((line) => [
      line.kind,
      line.oldLine,
      line.newLine,
    ]),
    [
      ["context", 2, 2],
      ["removed", 3, undefined],
      ["added", undefined, 3],
      ["added", undefined, 4],
      ["context", 4, 5],
      ["notice", undefined, undefined],
    ],
  );
  assert.equal(files[1].newPath, "测试 name.txt");
  assert.equal(files[1].status, "added");
  assert.equal(files[1].oldPath, null);
  assert.equal(files[2].status, "deleted");
  assert.equal(files[2].newPath, null);
  assert.equal(files[3].status, "renamed");
  assert.equal(files[3].newPath, "new name.txt");
  assert.equal(files[4].binary, true);
  assert.equal(files.map((file) => file.raw).join(""), original);
  assert.equal(parseUnifiedDiff(files[1].raw)[0].key, files[1].key);
});

test("header-looking changed text remains code and CRLF patches copy unchanged", () => {
  const raw =
    "--- a/sample.txt\r\n+++ b/sample.txt\r\n@@ -1 +1 @@\r\n--- header-like code\r\n+++ replacement-like code\r\n";
  const files = parseUnifiedDiff(raw);
  assert.equal(files.length, 1);
  assert.equal(files[0].raw, raw);
  assert.deepEqual(
    files[0].hunks[0].lines.map((line) => line.text),
    ["-- header-like code", "++ replacement-like code"],
  );
  assert.equal(parseUnifiedDiff("plain diff text").length, 0);
});
