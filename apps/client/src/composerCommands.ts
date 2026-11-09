import type { ProjectSkill, SkillOption } from "@mewdex/sdk";
import { t } from "./localization.ts";

export type ComposerCommandId =
  | "new"
  | "plan"
  | "edit"
  | "model"
  | "permissions"
  | "skills"
  | "settings";
export interface ComposerCommandEntry {
  id: string;
  name: string;
  label: string;
  description: string;
  source: "command" | "project" | "runtime";
  command?: ComposerCommandId;
  insert?: string;
}

export function composerCommandEntries(
  native: SkillOption[],
  project: ProjectSkill[],
): ComposerCommandEntry[] {
  const names = new Set<string>();
  const runtime = native.filter((skill) => {
    if (names.has(skill.name)) return false;
    names.add(skill.name);
    return true;
  });
  return [
    {
      id: "new",
      name: "new",
      label: t("新建对话"),
      description: t("在当前项目开始新对话"),
      source: "command",
      command: "new",
    },
    {
      id: "plan",
      name: "plan",
      label: t("方案讨论"),
      description: t("先讨论方案，使用只读访问"),
      source: "command",
      command: "plan",
    },
    {
      id: "edit",
      name: "edit",
      label: t("直接修改"),
      description: t("返回执行模式"),
      source: "command",
      command: "edit",
    },
    {
      id: "model",
      name: "model",
      label: t("模型与思考强度"),
      description: t("选择后续回合使用的模型"),
      source: "command",
      command: "model",
    },
    {
      id: "permissions",
      name: "permissions",
      label: t("访问权限"),
      description: t("设置后续回合的访问范围"),
      source: "command",
      command: "permissions",
    },
    {
      id: "skills",
      name: "skills",
      label: t("Skills"),
      description: t("浏览项目和 Hub 的可用 Skill"),
      source: "command",
      command: "skills",
    },
    {
      id: "settings",
      name: "settings",
      label: t("任务设置"),
      description: t("打开当前对话的任务设置"),
      source: "command",
      command: "settings",
    },
    ...project.map((skill) => ({
      id: `project:${skill.id}`,
      name: skill.name,
      label: skill.name,
      description: skill.description,
      source: "project" as const,
      insert:
        t("使用项目 Skill「{0}」（ID {1}）。", skill.name, skill.id) + " ",
    })),
    // A plain $name is also a native Codex explicit invocation. Keep its first
    // catalog match, as the runtime does when resolving a name across scopes.
    ...runtime.map((skill) => ({
      id: `runtime:${skill.name}`,
      name: skill.name,
      label: skill.name,
      description: skill.description,
      source: "runtime" as const,
      insert: `$${skill.name} `,
    })),
  ];
}

export function composerSlashQuery(text: string, start: number, end = start) {
  if (start !== end) return null;
  const before = text.slice(0, start);
  const match = /(?:^|\s)([/$])([^\s/$]*|skills[ \t]+[^\n/$]*)$/u.exec(before);
  if (!match) return null;
  const tokenStart = match.index + match[0].indexOf(match[1]);
  const skillsPrefix = match[2].match(/^skills(?:[ \t]+(.*))?$/u);
  return {
    start: tokenStart,
    end: start,
    query: skillsPrefix ? (skillsPrefix[1] ?? "") : match[2],
    skillsOnly: match[1] === "$" || !!skillsPrefix,
    commandsAllowed: !text.slice(0, tokenStart).trim() && match[1] === "/",
  };
}

export function filterComposerCommands(
  entries: ComposerCommandEntry[],
  query: NonNullable<ReturnType<typeof composerSlashQuery>>,
) {
  const words = query.query.toLocaleLowerCase().trim().split(/\s+/u);
  return entries.filter(
    (entry) =>
      (entry.source !== "command" ||
        (!query.skillsOnly && query.commandsAllowed)) &&
      words.every((word) =>
        `${entry.name} ${entry.label} ${entry.description}`
          .toLocaleLowerCase()
          .includes(word),
      ),
  );
}

export function typedComposerCommand(
  text: string,
  entries: ComposerCommandEntry[],
) {
  const match = /^\s*\/([^\s/]+)(?:[ \t]+([\s\S]*))?$/u.exec(text);
  if (!match) return null;
  const entry = entries.find((item) => item.name === match[1]);
  return entry ? { entry, rest: match[2] ?? "" } : null;
}
