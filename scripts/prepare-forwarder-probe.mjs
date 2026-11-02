// Preserve live development profiles; prepare separate profiles for the built-in route.
import { readFile, writeFile } from 'node:fs/promises';

const hub = process.argv.find(x => x.startsWith('--hub='))?.slice(6);
if (hub) {
  const u = new URL(hub);
  if (u.protocol !== 'https:' || u.username || u.password || u.search || u.hash) throw new Error('An explicit HTTPS Hub URL without credentials is required');
}
for (const alias of ['MewGullNest_1', 'MewGullNest_HighSpeed']) {
  const dir = `.local/remotes/${alias}`;
  const c = JSON.parse(await readFile(`${dir}/device.json`, 'utf8'));
  if (hub) c.hubUrl = hub;
  delete c.engine.proxyUrl;
  delete c.engine.proxyTokenFile;
  delete c.engine.proxyUsername;
  c.engine.useHubGateway = true;
  const path = `${dir}/device-hub-gateway.json`;
  await writeFile(path, JSON.stringify(c, null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify({ alias, deviceId: c.deviceId, profile: path, hub: c.hubUrl, modelCredentialsRead: false }));
}
