// Prepares public build dependencies on a connected host. No service deployment.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { mkdir, readFile, copyFile, open, unlink, writeFile } from 'node:fs/promises';
import { pipeline } from 'node:stream/promises';
import { Transform, Readable } from 'node:stream';
import { dirname, join, relative, resolve, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';

const run = promisify(execFile);
const root = fileURLToPath(new URL('..', import.meta.url));
const assets = join(root, '.state', 'go-source-check-assets');
await mkdir(assets, { recursive: true });
const lockPath = join(assets, 'prepare.lock');
const guard = await open(lockPath, 'wx');
try {
  const lock = JSON.parse(await readFile(join(root, 'tools/go-source-check.lock.json'), 'utf8'));
  const spec = lock.linuxAmd64;
  const archive = join(assets, spec.filename);
  const hash = async (path) => {
    const digest = createHash('sha256');
    for await (const chunk of createReadStream(path)) digest.update(chunk);
    return digest.digest('hex');
  };
  let present = false;
  try {
    const existing = await hash(archive);
    if (existing !== spec.sha256) throw new Error('Existing Go archive failed integrity verification; retained for inspection');
    present = true;
  } catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (!present) {
    const response = await fetch(spec.url, { signal: AbortSignal.timeout(120000) });
    if (!response.ok || !response.body) throw new Error('Official Go archive download failed');
    let bytes = 0;
    const temporary = `${archive}.partial`;
    await pipeline(Readable.fromWeb(response.body), new Transform({
      transform(chunk, _encoding, callback) {
        bytes += chunk.length;
        callback(bytes > spec.bytes ? new Error('Go archive exceeds pinned size') : null, chunk);
      },
    }), createWriteStream(temporary, { flags: 'wx' }));
    if (bytes !== spec.bytes || await hash(temporary) !== spec.sha256) throw new Error('Official Go archive checksum did not match the pin');
    // The final name is created once; a failed preparation never overwrites it.
    await copyFile(temporary, archive, 1);
    await unlink(temporary);
  }
  const { stdout: cacheOutput } = await run('go', ['env', 'GOMODCACHE'], { cwd: root });
  const cache = resolve(cacheOutput.trim());
  const { stdout: list } = await run('go', ['list', '-m', '-f', '{{.Path}} {{.Version}}', 'all'], { cwd: root });
  const modules = list.trim().split(/\r?\n/).map(line => line.trim().split(/\s+/)).filter(parts => parts.length === 2);
  for (const [path, version] of modules) {
    const { stdout } = await run('go', ['mod', 'download', '-json', `${path}@${version}`], { cwd: root });
    const entry = JSON.parse(stdout);
    if (!entry.Zip || !entry.Sum || !entry.GoModSum) throw new Error('A module has no verified public archive');
    for (const source of [entry.Zip, `${entry.Zip}hash`, entry.GoMod, entry.Info]) {
      const local = relative(cache, resolve(source));
      if (isAbsolute(local) || local === '..' || local.startsWith(`..${process.platform === 'win32' ? '\\' : '/'}`)) throw new Error('Module archive escaped its cache');
      const target = join(assets, 'modules', local);
      await mkdir(dirname(target), { recursive: true });
      await copyFile(source, target);
    }
  }
  const moduleArchive = join(assets, 'modules.tar.gz');
  await run('tar', ['-czf', moduleArchive, '-C', join(assets, 'modules'), '.'], { cwd: root });
  const report = {
    at: new Date().toISOString(), version: lock.version, archive, archiveSha256: spec.sha256,
    moduleArchive, moduleArchiveSha256: await hash(moduleArchive), modules: modules.length,
    credentialsRead: false, serviceChanged: false,
  };
  await writeFile(join(assets, 'manifest.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
} finally {
  await guard.close();
  await unlink(lockPath);
}
