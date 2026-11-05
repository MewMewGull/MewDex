import type { Project } from "@mewdex/sdk";

// Message paths only navigate the scoped file viewer. They never reach a native
// opener, a shell, or an automatic file upload.
export interface ProjectFileLocation {
  path: string;
  line?: number;
}

function fileLocation(path: string, fragment: string): ProjectFileLocation {
  const suffix = path.match(/:(\d+)(?::\d+)?$/);
  const anchor = fragment.match(/^L(\d+)(?:C\d+)?(?:-L?\d+(?:C\d+)?)?$/i);
  const line = Number(anchor?.[1] ?? suffix?.[1]);
  return {
    path: suffix ? path.slice(0, suffix.index) : path,
    ...(line > 0 ? { line } : {}),
  };
}

export function projectFileLocation(
  raw: string | undefined,
): ProjectFileLocation | null {
  if (!raw || raw.startsWith("#") || raw.startsWith("//")) return null;
  const path = raw.split("#", 1)[0];
  const fragment = raw.includes("#") ? raw.slice(raw.indexOf("#") + 1) : "";
  if (/^file:\/\//i.test(path)) {
    try {
      const url = new URL(path);
      if (url.hostname && url.hostname !== "localhost") return null;
      return fileLocation(
        decodeURIComponent(url.pathname).replace(/^\/([a-z]:\/)/i, "$1"),
        fragment,
      );
    } catch {
      return null;
    }
  }
  const filenameLocation = /^[^:/\\]+\.[^:/\\]+:\d+(?::\d+)?$/.test(path);
  if (
    /^[a-z][a-z0-9+.-]*:/i.test(path) &&
    !/^[a-z]:[\\/]/i.test(path) &&
    !filenameLocation
  )
    return null;
  try {
    return fileLocation(
      decodeURIComponent(path)
        .replace(/\\/g, "/")
        .replace(/^\/([a-z]:\/)/i, "$1"),
      fragment,
    );
  } catch {
    return null;
  }
}

export function projectFileReference(raw: string | undefined): string | null {
  return projectFileLocation(raw)?.path ?? null;
}

export function relativeProjectPath(path: string, project: string): string {
  if (!project) return path;
  const root = project.replace(/\\/g, "/").replace(/\/$/, "");
  const compare = /^[a-z]:\//i.test(root) ? path.toLowerCase() : path;
  const base = /^[a-z]:\//i.test(root) ? root.toLowerCase() : root;
  if (compare === base) return ".";
  return compare.startsWith(base + "/") ? path.slice(root.length + 1) : path;
}

export function projectFileTarget(path: string, project?: Project) {
  let target = {
    path: relativeProjectPath(path, project?.path ?? ""),
    directory: undefined as number | undefined,
  };
  let longestRoot = 0;
  project?.directories?.forEach((binding, index) => {
    const relative = relativeProjectPath(path, binding.path);
    if (relative !== path && binding.path.length > longestRoot) {
      target = { path: relative, directory: index };
      longestRoot = binding.path.length;
    }
  });
  return target;
}

export function parentProjectPath(path: string): string {
  const normalized = path.replace(/\\/g, "/").replace(/\/$/, "");
  return normalized.includes("/")
    ? normalized.slice(0, normalized.lastIndexOf("/")) || "/"
    : ".";
}
