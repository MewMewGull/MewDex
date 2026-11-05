import fs from "node:fs";
import path from "node:path";
import ts from "typescript";

const root = path.resolve(import.meta.dirname, "../apps/client/src");
export function applicationMessages() {
  const messages = new Set();
  for (const file of fs.readdirSync(root, { recursive: true })) {
    if (!/\.tsx?$/.test(file) || file === "localization.ts") continue;
    const full = path.join(root, file);
    const ast = ts.createSourceFile(
      full,
      fs.readFileSync(full, "utf8"),
      ts.ScriptTarget.Latest,
      true,
    );
    function visit(node) {
      if (
        ts.isCallExpression(node) &&
        node.expression.getText(ast) === "t" &&
        node.arguments[0] &&
        ts.isStringLiteralLike(node.arguments[0])
      )
        messages.add(node.arguments[0].text);
      ts.forEachChild(node, visit);
    }
    visit(ast);
  }
  // Storage errors remain source messages until they are rendered.
  messages.add("托盘语言未能更新，请重新选择语言。");
  messages.add("语言设置未保存，关闭工作台后可能恢复原设置。");
  messages.add("请检查标记的回答。");
  return [...messages].sort();
}

export function checkCatalogs() {
  const keys = applicationMessages();
  const errors = [];
  for (const language of ["zh-TW", "en", "ja", "ko"]) {
    const catalog = JSON.parse(
      fs.readFileSync(path.join(root, "locales", `${language}.json`), "utf8"),
    );
    for (const key of keys) {
      const value = catalog[key];
      if (typeof value !== "string" || !value.trim()) {
        errors.push(`${language}: missing ${JSON.stringify(key)}`);
        continue;
      }
      const placeholders = (text) =>
        [...text.matchAll(/\{\d+\}/g)]
          .map((match) => match[0])
          .sort()
          .join(",");
      if (placeholders(key) !== placeholders(value))
        errors.push(
          `${language}: invalid placeholders for ${JSON.stringify(key)}`,
        );
    }
  }
  return { count: keys.length, errors };
}

if (process.argv[1] === import.meta.filename) {
  if (process.argv.includes("--extract")) {
    process.stdout.write(JSON.stringify(applicationMessages(), null, 2) + "\n");
  } else {
    const { count, errors } = checkCatalogs();
    if (errors.length) {
      console.error(errors.join("\n"));
      process.exitCode = 1;
    } else
      console.log(
        `All four translated catalogs cover ${count} application messages with matching placeholders.`,
      );
  }
}
