// Packages a prepared Windows desktop build; never installs it on this machine.
import { createHash } from 'node:crypto';
import { readFile, writeFile, mkdir, readdir, copyFile, lstat } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import path from 'node:path';

if (process.platform !== 'win32') throw new Error('Build the Windows installer on Windows');
const root = path.resolve(import.meta.dirname, '..');
const input = process.argv[2];
if (!input || ![3,4].includes(process.argv.length)) throw new Error('Usage: node scripts/build-windows-installer.mjs <prepared desktop build folder> [installer-name.exe]');
const installerName = process.argv[3] ?? 'MewDex-Windows-x64-20261102-Hub-Setup.exe';
if (!/^[A-Za-z0-9_.-]+\.exe$/.test(installerName)) throw new Error('Installer name must be a plain .exe filename');
const source = path.resolve(root, input);
const relative = path.relative(root, source);
if (!relative || relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) throw new Error('Build input must stay in this workspace');
const stage = path.join(root, '.state', `hub-installer-${Date.now()}`);
const payload = path.join(stage, 'payload');
await mkdir(payload, { recursive: true });
function run(exe, args, extra = {}) {
  const result = spawnSync(exe, args, { cwd: root, encoding: 'utf8', windowsHide: true, maxBuffer: 4 * 1024 * 1024, ...extra });
  if (result.status !== 0) throw new Error(`${path.basename(exe)} failed: ${(result.stderr || result.stdout || result.error?.message || '').slice(-3000)}`);
  return result.stdout;
}
const files = [];
async function copy(name) {
  const sourceFile = path.join(source, name);
  const info = await lstat(sourceFile);
  if (info.isDirectory()) { for (const entry of await readdir(sourceFile)) await copy(path.join(name, entry)); return; }
  if (!info.isFile()) throw new Error('Installer payload contains a link or special file');
  await mkdir(path.dirname(path.join(payload, name)), { recursive: true });
  await copyFile(sourceFile, path.join(payload, name)); files.push(name);
}
for (const name of ['mewdex-client.exe', 'mewdex-node.exe', 'engine']) await copy(name);
const helper = await readFile(path.join(payload, 'mewdex-node.exe'));
if (helper.readUInt16LE(helper.readUInt32LE(0x3c) + 92) !== 2) throw new Error('The Windows Hub must use the GUI subsystem to prevent a login console');
for (const name of await readdir(source)) if (/\.dll$/i.test(name)) await copy(name);
const lock = JSON.parse(await readFile(path.join(root, 'tools/windows-installer.lock.json'), 'utf8'));
const tools = path.join(root, '.local', 'toolchains'); await mkdir(tools, { recursive: true });
const zip = path.join(tools, 'nsis-3.13.zip');
let archive;
try { archive = await readFile(zip); } catch (error) { if (error.code !== 'ENOENT') throw error; }
if (!archive) {
  const response = await fetch(lock.nsis.url, { signal: AbortSignal.timeout(60000) });
  if (!response.ok) throw new Error('NSIS download failed');
  archive = Buffer.from(await response.arrayBuffer());
  if (createHash('sha1').update(archive).digest('hex') !== lock.nsis.sha1) throw new Error('NSIS archive hash mismatch');
  await writeFile(zip, archive, { flag: 'wx' });
}
if (createHash('sha1').update(archive).digest('hex') !== lock.nsis.sha1) throw new Error('Cached NSIS archive hash mismatch');
const nsis = path.join(tools, 'nsis-3.13', 'makensis.exe');
try { await lstat(nsis); } catch (error) {
  if (error.code !== 'ENOENT') throw error;
  const entries = run('tar', ['-tf', zip]).trim().split(/\r?\n/);
  if (entries.some(name => !name.startsWith('nsis-3.13/') || name.includes('..') || name.includes('\\'))) throw new Error('Unexpected NSIS archive paths');
  run('tar', ['-xf', zip, '-C', tools]);
}
const bootstrap = path.join(tools, 'MicrosoftEdgeWebview2Setup.exe');
try { await lstat(bootstrap); } catch (error) {
  if (error.code !== 'ENOENT') throw error;
  const response = await fetch(lock.webview2Bootstrapper, { signal: AbortSignal.timeout(60000) });
  if (!response.ok) throw new Error('Microsoft WebView2 bootstrapper download failed');
  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.length > 8 * 1024 * 1024) throw new Error('Unexpected WebView2 bootstrapper size');
  await writeFile(bootstrap, bytes, { flag: 'wx' });
}
run('powershell.exe', ['-NoProfile', '-NonInteractive', '-File', path.join(root, 'scripts/windows/verify-microsoft-file.ps1'), '-Path', bootstrap], { env: { ...process.env, PSModulePath: path.join(process.env.SystemRoot, 'System32/WindowsPowerShell/v1.0/Modules') } });
const escape = value => value.replaceAll('$', () => '$$').replaceAll('"', () => '$\\"');
const manifest = path.join(stage, 'uninstall-files.nsh');
const directories = new Set();
for (const file of files) { let dir = path.dirname(file); while (dir !== '.') { directories.add(dir); dir = path.dirname(dir); } }
await writeFile(manifest, files.map(file => `Delete "$INSTDIR\\${escape(file)}"`).join('\n') + '\n' + [...directories].sort((a,b) => b.length-a.length).map(dir => `RMDir "$INSTDIR\\${escape(dir)}"`).join('\n') + '\n');
const output = path.join(root, 'dist/windows', installerName);
await mkdir(path.dirname(output), { recursive: true });
run(nsis, ['/INPUTCHARSET', 'UTF8', '/V2', `/DPAYLOAD=${payload}`, `/DMANIFEST=${manifest}`, `/DBOOTSTRAPPER=${bootstrap}`, `/DOUTPUT=${output}`, path.join(root, 'scripts/windows/installer.nsi')]);
const result = { output, files: files.length, sha256: createHash('sha256').update(await readFile(output)).digest('hex'), installed: false };
await writeFile(path.join(stage, 'build.json'), JSON.stringify(result, null, 2)+'\n');
console.log(JSON.stringify(result));
