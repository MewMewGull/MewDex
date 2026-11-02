export interface DiffLine {
  kind: "context" | "added" | "removed" | "notice";
  text: string;
  oldLine?: number;
  newLine?: number;
}
export interface DiffHunk {
  header: string;
  lines: DiffLine[];
}
export interface DiffFile {
  key: string;
  oldPath: string | null;
  newPath: string | null;
  status: "added" | "deleted" | "renamed" | "modified";
  added: number;
  removed: number;
  binary: boolean;
  metadata: string[];
  hunks: DiffHunk[];
  raw: string;
}

// Git quotes paths using C escapes and UTF-8 octal bytes. These names are only
// viewer references; opening a file still goes through the scoped file API.
function pathText(raw: string, prefix = true): string | null {
  let path = raw;
  if (raw.startsWith('"')) {
    const quoted = /^"((?:[^"\\]|\\.)*)"/.exec(raw);
    if (!quoted) return null;
    const bytes: number[] = [];
    const encoder = new TextEncoder();
    const escaped: Record<string, string> = {
      a: "\x07",
      b: "\b",
      f: "\f",
      n: "\n",
      r: "\r",
      t: "\t",
      v: "\v",
      '"': '"',
      "\\": "\\",
    };
    const body = quoted[1];
    for (let i = 0; i < body.length; ) {
      if (body[i] === "\\") {
        const octal = /^[0-7]{1,3}/.exec(body.slice(i + 1));
        if (octal) {
          bytes.push(parseInt(octal[0], 8));
          i += octal[0].length + 1;
        } else {
          bytes.push(...encoder.encode(escaped[body[i + 1]] ?? body[i + 1]));
          i += 2;
        }
      } else {
        const character = String.fromCodePoint(body.codePointAt(i)!);
        bytes.push(...encoder.encode(character));
        i += character.length;
      }
    }
    path = new TextDecoder().decode(new Uint8Array(bytes));
  } else path = raw.split("\t", 1)[0];
  if (path === "/dev/null") return null;
  return prefix ? path.replace(/^[ab]\//, "") : path;
}

export function parseUnifiedDiff(raw: string): DiffFile[] {
  const lines = raw.split("\n");
  const offsets: number[] = [];
  let offset = 0;
  for (const line of lines) {
    offsets.push(offset);
    offset += line.length + 1;
  }
  const files: DiffFile[] = [];
  let file: DiffFile | null = null;
  let start = 0;
  let headers = false;
  let hunk: DiffHunk | null = null;
  let oldLine = 0,
    newLine = 0,
    oldRemaining = 0,
    newRemaining = 0;
  function finish(end: number) {
    if (!file) return;
    file.raw = raw.slice(offsets[start], offsets[end] ?? raw.length);
    file.status =
      !file.oldPath && file.newPath
        ? "added"
        : file.oldPath && !file.newPath
          ? "deleted"
          : file.oldPath !== file.newPath
            ? "renamed"
            : file.status;
    file.key =
      file.newPath || file.oldPath
        ? "path:" + (file.newPath ?? file.oldPath)
        : "unknown:" + files.length;
    files.push(file);
    file = null;
  }
  function begin(index: number): DiffFile {
    finish(index);
    start = index;
    const current: DiffFile = {
      key: "",
      oldPath: null,
      newPath: null,
      status: "modified",
      added: 0,
      removed: 0,
      binary: false,
      metadata: [],
      hunks: [],
      raw: "",
    };
    headers = false;
    hunk = null;
    return current;
  }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].replace(/\r$/, "");
    if (line.startsWith("diff --git ")) {
      file = begin(i);
      const paths = /^("(?:[^"\\]|\\.)*"|\S+) ("(?:[^"\\]|\\.)*"|\S+)$/.exec(
        line.slice(11),
      );
      if (paths) {
        file!.oldPath = pathText(paths[1]);
        file!.newPath = pathText(paths[2]);
      }
      continue;
    }
    const range = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(line);
    if (range) {
      if (!file) file = begin(i);
      hunk = { header: line, lines: [] };
      file!.hunks.push(hunk);
      oldLine = Number(range[1]);
      newLine = Number(range[3]);
      oldRemaining = Number(range[2] ?? 1);
      newRemaining = Number(range[4] ?? 1);
      continue;
    }
    if (file && hunk) {
      if (line.startsWith("\\ ")) {
        hunk.lines.push({ kind: "notice", text: line });
        continue;
      }
      if (oldRemaining > 0 || newRemaining > 0) {
        if (line.startsWith("+")) {
          hunk.lines.push({
            kind: "added",
            text: line.slice(1),
            newLine: newLine++,
          });
          newRemaining--;
          file.added++;
          continue;
        }
        if (line.startsWith("-")) {
          hunk.lines.push({
            kind: "removed",
            text: line.slice(1),
            oldLine: oldLine++,
          });
          oldRemaining--;
          file.removed++;
          continue;
        }
        if (line.startsWith(" ")) {
          hunk.lines.push({
            kind: "context",
            text: line.slice(1),
            oldLine: oldLine++,
            newLine: newLine++,
          });
          oldRemaining--;
          newRemaining--;
          continue;
        }
      }
    }
    if (line.startsWith("--- ") && lines[i + 1]?.startsWith("+++ ")) {
      if (!file || headers) file = begin(i);
      file!.oldPath = pathText(line.slice(4));
      file!.newPath = pathText(lines[++i].replace(/\r$/, "").slice(4));
      headers = true;
      hunk = null;
      continue;
    }
    if (!file || !line) continue;
    if (line.startsWith("rename from "))
      file.oldPath = pathText(line.slice(12), false);
    if (line.startsWith("rename to "))
      file.newPath = pathText(line.slice(10), false);
    if (line.startsWith("new file mode ")) {
      file.oldPath = null;
      file.status = "added";
    }
    if (line.startsWith("deleted file mode ")) {
      file.newPath = null;
      file.status = "deleted";
    }
    if (line.startsWith("Binary files ") || line === "GIT binary patch")
      file.binary = true;
    file.metadata.push(line);
  }
  finish(lines.length);
  return files;
}
