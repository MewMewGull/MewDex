import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createServer as createHTTPServer } from "node:http";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createServer } from "vite";

test("thumbnail images crop a centered square and detailed previews show the full image", () => {
  const stylesheet = readFileSync(
    new URL("../src/style.css", import.meta.url),
    "utf8",
  );
  const thumbnail = stylesheet.match(
    /\.attachment-thumbnail img\s*\{([^}]+)\}/,
  )?.[1];
  assert.match(thumbnail, /object-fit:\s*cover\s*;/);
  assert.match(thumbnail, /object-position:\s*center\s*;/);
  assert.match(thumbnail, /width:\s*100%\s*;/);
  assert.match(thumbnail, /height:\s*100%\s*;/);
  const detail = stylesheet.match(/\.image-preview img\s*\{([^}]+)\}/)?.[1];
  assert.match(detail, /object-fit:\s*contain\s*;/);
});

test("composer images expose clickable thumbnails for uploaded and pending files", async () => {
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("..", import.meta.url)),
    server: {
      middlewareMode: true,
      hmr: { server: createHTTPServer() },
      watch: null,
    },
    optimizeDeps: { noDiscovery: true, include: [] },
    appType: "custom",
    esbuild: { jsx: "automatic" },
  });
  const queries = new QueryClient();
  try {
    const { AttachmentList } = await server.ssrLoadModule(
      "/src/components/AttachmentList.tsx",
    );
    const { AttachmentUploadList } = await server.ssrLoadModule(
      "/src/components/AttachmentUploadList.tsx",
    );
    const { canPreviewImage } = await server.ssrLoadModule(
      "/src/attachmentFiles.ts",
    );
    assert.equal(canPreviewImage(" IMAGE/PNG ; charset=binary"), true);
    assert.equal(canPreviewImage("image/svg+xml"), false);
    const render = (component, props) =>
      renderToStaticMarkup(
        createElement(
          QueryClientProvider,
          { client: queries },
          createElement(component, props),
        ),
      );
    const image = {
      id: "image",
      name: "pasted.png",
      mediaType: "image/png",
      size: 1024,
    };
    const document = {
      id: "text",
      name: "notes.txt",
      mediaType: "text/plain",
      size: 512,
    };
    const draft = render(AttachmentList, {
      client: {},
      items: [{ id: image.id, metadata: image }],
      onRemove() {},
    });
    assert.match(draft, /file-card draft is-image/);
    assert.match(draft, /attachment-thumbnail/);
    assert.match(draft, /aria-label="查看附件 pasted.png"/);
    assert.match(draft, /aria-label="移除附件 pasted.png"/);
    assert.doesNotMatch(draft, /<strong\b/);
    const regular = render(AttachmentList, {
      client: {},
      items: [{ id: document.id, metadata: document }],
      onRemove() {},
    });
    assert.match(regular, /<strong[^>]*>notes.txt<\/strong>/);
    assert.doesNotMatch(regular, /attachment-thumbnail/);
    const history = render(AttachmentList, {
      client: {},
      items: [{ id: image.id, metadata: image }],
    });
    assert.match(history, /<strong[^>]*>pasted.png<\/strong>/);
    const pending = render(AttachmentUploadList, {
      client: {},
      items: [
        {
          id: "upload",
          name: image.name,
          size: image.size,
          state: "uploading",
          preview: new File(["image"], image.name, { type: image.mediaType }),
        },
      ],
      onRemove() {},
      onRetry() {},
    });
    assert.match(pending, /attachment-upload-item is-uploading is-image/);
    assert.match(pending, /aria-label="查看附件 pasted.png"/);
    assert.match(pending, /aria-label="取消上传 pasted.png"/);
    assert.match(pending, /正在上传/);
    assert.match(pending, /class="sr-only">pasted.png<\/strong>/);
  } finally {
    queries.clear();
    await server.close();
  }
});
