import { test } from "node:test";
import assert from "node:assert/strict";
import {
  projectFileReference,
  projectFileLocation,
  relativeProjectPath,
  parentProjectPath,
  projectFileTarget,
} from "../src/projectFileLinks.ts";

test("Hub file references select the directory and native path of their owning node", () => {
  const project = {
    id: "hp_sott",
    name: "Sott",
    path: "",
    directories: [
      { deviceId: "win", projectId: "game", path: "D:\\Sott", name: "Windows" },
      {
        deviceId: "mac",
        projectId: "game",
        path: "/Users/me/Sott",
        name: "Mac",
      },
    ],
  };
  assert.deepEqual(projectFileTarget("D:/Sott/Source/a.cpp", project), {
    path: "Source/a.cpp",
    directory: 0,
  });
  assert.deepEqual(projectFileTarget("/Users/me/Sott/Source/a.cpp", project), {
    path: "Source/a.cpp",
    directory: 1,
  });
  assert.deepEqual(projectFileTarget("/outside/a.cpp", project), {
    path: "/outside/a.cpp",
    directory: undefined,
  });
});

test("project links navigate paths without treating executable URL schemes as files", () => {
  assert.equal(
    projectFileReference("D:\\work\\报告.txt:12:3"),
    "D:/work/报告.txt",
  );
  assert.equal(
    projectFileReference("file:///D:/work/report%20one.md#L12"),
    "D:/work/report one.md",
  );
  assert.equal(projectFileReference("/work/report.md:12"), "/work/report.md");
  assert.equal(projectFileReference("docs/report.md#L12"), "docs/report.md");
  for (const href of [
    "javascript:alert(1)",
    "data:text/html,hi",
    "https://example.com/report",
    "file://other-computer/report",
    "//other-computer/report",
    "#heading",
  ])
    assert.equal(projectFileReference(href), null);
  assert.equal(
    relativeProjectPath("D:/work/report.txt", "d:\\work"),
    "report.txt",
  );
  assert.equal(
    relativeProjectPath("/other/report.txt", "/work"),
    "/other/report.txt",
  );
  assert.equal(
    relativeProjectPath("/other/report.txt", ""),
    "/other/report.txt",
  );
  assert.equal(parentProjectPath("docs/report.txt"), "docs");
});

test("file locations retain the referenced line without passing it as a file path", () => {
  assert.deepEqual(projectFileLocation("D:\\work\\报告.txt:12:3"), {
    path: "D:/work/报告.txt",
    line: 12,
  });
  assert.deepEqual(
    projectFileLocation("file:///D:/work/report%20one.md#L23-L27"),
    {
      path: "D:/work/report one.md",
      line: 23,
    },
  );
  assert.deepEqual(projectFileLocation("src/main.ts:42#L7C5"), {
    path: "src/main.ts",
    line: 7,
  });
  assert.deepEqual(projectFileLocation("docs/a.md#overview"), {
    path: "docs/a.md",
  });
  assert.deepEqual(projectFileLocation("empty.txt:0"), { path: "empty.txt" });
  assert.deepEqual(projectFileLocation("README.md:12"), {
    path: "README.md",
    line: 12,
  });
  assert.deepEqual(projectFileLocation("./Dockerfile:12"), {
    path: "./Dockerfile",
    line: 12,
  });
  for (const value of [
    "#L12",
    "https://example.com/a#L12",
    "javascript:12",
    "data:12",
    "file://other/a#L12",
    "//other/a:12",
  ])
    assert.equal(projectFileLocation(value), null);
});
