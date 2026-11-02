import { FolderPlus, X } from "lucide-react";
import type { Project } from "@mewdex/sdk";

export function ProjectDirectoryEditor({
  projects,
  disabled,
  onChange,
  onAdd,
}: {
  projects: Project[];
  disabled: boolean;
  onChange: (change: (projects: Project[]) => Project[]) => void;
  onAdd: () => void;
}) {
  return (
    <section className="local-projects">
      <p className="muted">选择这台电脑上要处理的项目目录。</p>
      {projects.map((project, index) => (
        <article key={`${project.path}:${index}`}>
          <input
            aria-label={`项目 ${index + 1} 名称`}
            value={project.name}
            disabled={disabled}
            onChange={(event) =>
              onChange((items) =>
                items.map((item, position) =>
                  position === index
                    ? { ...item, name: event.target.value }
                    : item,
                ),
              )
            }
          />
          <code>{project.path}</code>
          <button
            type="button"
            className="icon-button"
            aria-label={`移除项目 ${project.name}`}
            disabled={disabled}
            onClick={() =>
              onChange((items) =>
                items.filter((_, position) => position !== index),
              )
            }
          >
            <X size={15} />
          </button>
        </article>
      ))}
      <button
        type="button"
        className="project-add-button"
        disabled={disabled}
        onClick={onAdd}
      >
        <FolderPlus size={16} />
        {projects.length ? "添加项目目录" : "选择项目目录"}
      </button>
    </section>
  );
}
