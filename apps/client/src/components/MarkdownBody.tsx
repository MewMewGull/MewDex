import { useMemo, useRef, useState, type ReactNode } from "react";
import Markdown, { defaultUrlTransform, type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import * as ContextMenu from "@radix-ui/react-context-menu";
import { Image as ImageIcon } from "lucide-react";
import { openExternalLink } from "../platform";
import { CopyButton } from "./CopyButton";
import { projectFileLocation, projectFileReference } from "../projectFileLinks";

function CodeBlock({ children }: { children?: ReactNode }) {
  const code = useRef<HTMLPreElement>(null);
  return (
    <div className="code-block">
      <div className="code-block-toolbar">
        <span>代码</span>
        <CopyButton
          text={() => code.current!.textContent ?? ""}
          label="复制代码"
        />
      </div>
      <pre ref={code}>{children}</pre>
    </div>
  );
}

function MessageLink({
  href,
  children,
  onFileReference,
}: {
  href?: string;
  children?: ReactNode;
  onFileReference?: (path: string, action?: string) => void;
}) {
  const [error, setError] = useState("");
  const reference = projectFileLocation(href);
  if (reference?.path && onFileReference)
    return (
      <ContextMenu.Root>
        <ContextMenu.Trigger asChild>
          <a
            href={href}
            title={`打开项目文件：${reference.path}${reference.line ? ` · 第 ${reference.line} 行` : ""}；右键选择打开方式`}
            onClick={(event) => {
              event.preventDefault();
              onFileReference(href!);
            }}
            onAuxClick={(event) => {
              if (event.button === 1) {
                event.preventDefault();
                onFileReference(href!, "panel");
              }
            }}
          >
            {children}
          </a>
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content
            className="session-context-menu"
            collisionPadding={8}
          >
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onFileReference(href!)}
            >
              使用默认应用打开
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onFileReference(href!, "open-with")}
            >
              选择打开方式…
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onFileReference(href!, "reveal")}
            >
              在资源管理器中显示
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => onFileReference(href!, "panel")}
            >
              在项目文件中查看
            </ContextMenu.Item>
            <ContextMenu.Item
              className="session-context-item"
              onSelect={() => {
                void navigator.clipboard.writeText(reference.path);
              }}
            >
              复制路径
            </ContextMenu.Item>
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>
    );
  if (!href?.startsWith("https://"))
    return <span title={href}>{children}</span>;
  return (
    <>
      <a
        href={href}
        title={href}
        onClick={(event) => {
          event.preventDefault();
          void openExternalLink(href).catch((cause: Error) =>
            setError(cause.message),
          );
        }}
      >
        {children}
      </a>
      {error && (
        <small className="inline-link-error" role="alert">
          {error}
        </small>
      )}
    </>
  );
}

const components: Components = {
  pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
  a: ({ href, children }) => <MessageLink href={href}>{children}</MessageLink>,
  table: ({ children }) => (
    <div className="markdown-table">
      <table>{children}</table>
    </div>
  ),
  img: ({ src, alt }) => (
    <MessageLink href={typeof src === "string" ? src : undefined}>
      <span className="message-image-reference">
        <ImageIcon size={14} />
        {alt || "查看图片"}
      </span>
    </MessageLink>
  ),
};
const plugins = [remarkGfm];

export function MarkdownBody({
  text,
  onFileReference,
}: {
  text: string;
  onFileReference?: (path: string, action?: string) => void;
}) {
  const renderers = useMemo<Components>(
    () => ({
      ...components,
      a: ({ href, children }) => (
        <MessageLink href={href} onFileReference={onFileReference}>
          {children}
        </MessageLink>
      ),
      img: ({ src, alt }) => (
        <MessageLink
          href={typeof src === "string" ? src : undefined}
          onFileReference={onFileReference}
        >
          <span className="message-image-reference">
            <ImageIcon size={14} />
            {alt || "查看图片"}
          </span>
        </MessageLink>
      ),
    }),
    [onFileReference],
  );
  return (
    <div className="markdown-body">
      <Markdown
        remarkPlugins={plugins}
        components={renderers}
        urlTransform={(url) =>
          projectFileReference(url) ? url : defaultUrlTransform(url)
        }
        skipHtml
      >
        {text}
      </Markdown>
    </div>
  );
}
