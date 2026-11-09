import { test } from "node:test";
import assert from "node:assert/strict";
import {
  composerCommandEntries,
  composerSlashQuery,
  filterComposerCommands,
  typedComposerCommand,
} from "../src/composerCommands.ts";

const entries = composerCommandEntries(
  [
    {
      name: "pdf",
      description: "Read PDF documents",
      path: "/one/SKILL.md",
      scope: "user",
    },
    {
      name: "pdf",
      description: "Shadowed PDF skill",
      path: "/two/SKILL.md",
      scope: "system",
    },
  ],
  [{ id: "release", name: "发布检查", description: "项目发布工作流" }],
);

test("plugin skill completion retains its runtime namespace", () => {
  const catalog = composerCommandEntries(
    [
      {
        name: "demo:review",
        path: "/plugins/demo/SKILL.md",
        scope: "user",
        description: "Plugin review",
      },
    ],
    [],
  );
  const query = composerSlashQuery("/demo:review", 12);
  const selected = filterComposerCommands(catalog, query);
  assert.equal(selected[0].insert, "$demo:review ");
  assert.equal(
    typedComposerCommand("/demo:review check", catalog).rest,
    "check",
  );
});

test("slash completion observes the caret, supports skills and leaves paths and selections alone", () => {
  const query = (text) => composerSlashQuery(text, text.length);
  assert.equal(query("https://example.org/path"), null);
  assert.equal(query("/usr/bin"), null);
  assert.equal(composerSlashQuery("/pdf", 1, 3), null);
  assert.equal(query("/").commandsAllowed, true);
  assert.equal(query("请使用 /pdf").commandsAllowed, false);
  assert.equal(query("/skills pdf").query, "pdf");
  assert.equal(query("/skills pdf").skillsOnly, true);
  assert.equal(query("$pdf").skillsOnly, true);
  assert.deepEqual(
    filterComposerCommands(entries, query("/pdf")).map((entry) => entry.id),
    ["runtime:pdf"],
  );
  assert.deepEqual(
    filterComposerCommands(entries, query("/发布")).map((entry) => entry.id),
    ["project:release"],
  );
  assert.ok(
    filterComposerCommands(entries, query("/skills")).every(
      (entry) => entry.source !== "command",
    ),
  );
  assert.equal(
    filterComposerCommands(entries, composerSlashQuery("/plan trailing", 5))[0]
      .command,
    "plan",
  );
});

test("typed commands and skills use the same catalog and keep their remaining input", () => {
  assert.equal(
    typedComposerCommand("/plan 保留需求", entries).rest,
    "保留需求",
  );
  assert.equal(
    typedComposerCommand("/plan 保留需求", entries).entry.command,
    "plan",
  );
  assert.equal(
    typedComposerCommand("/pdf read it", entries).entry.insert,
    "$pdf ",
  );
  assert.match(
    typedComposerCommand("/发布检查", entries).entry.insert,
    /release/,
  );
  assert.equal(typedComposerCommand("/unknown", entries), null);
  assert.equal(typedComposerCommand("text /new", entries), null);
  assert.equal(typedComposerCommand("/usr/bin", entries), null);
});
