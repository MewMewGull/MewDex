// Generates only project-local development profiles. Never reads model credentials.
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { localHub } from './lib/local-hub.mjs';

const { request } = await localHub();
const identities = await request('/v1/identities');
const specifications = [
  { alias: 'MewGullNest_1', deviceId: 'mewdex-risk-nest-1', name: 'MewGullNest_1 · 开发节点' },
  { alias: 'MewGullNest_HighSpeed', deviceId: 'mewdex-risk-nest-highspeed', name: 'MewGullNest_HighSpeed · 开发节点' },
];
if (process.argv.includes('--include-mac')) specifications.push({ alias:'SOTT-MacMini', deviceId:'mewdex-risk-mac-mini',name:'Mac Mini · 开发节点',root:'/Users/mewgull/MewDex/.state/risk-20261028',vendor:'aarch64-apple-darwin',direct:true });
const only = process.argv.find(arg => arg.startsWith('--only='))?.slice(7);
const hub = process.argv.find(arg => arg.startsWith('--hub='))?.slice(6) ?? 'http://127.0.0.1:47830';
const endpoint = new URL(hub);
if (endpoint.username || endpoint.password || endpoint.search || endpoint.hash || (endpoint.protocol !== 'https:' && hub !== 'http://127.0.0.1:47830')) throw new Error('An explicit HTTPS Hub address is required outside the loopback development link');
if (only && !specifications.some(spec => spec.alias === only)) throw new Error('Unknown requested development device');
for (const spec of specifications.filter(spec => !only || spec.alias === only)) {
  const root=spec.root??'/root/MewDex/.state/risk-20261028';
  const dir = `.local/remotes/${spec.alias}`; await mkdir(dir, { recursive: true });
  let paired;
  try { paired = JSON.parse(await readFile(`${dir}/identity.json`, 'utf8')); } catch {
    if (identities.some((id) => id.role === 'executor' && id.deviceId === spec.deviceId)) throw new Error(`An executor already owns ${spec.deviceId}; restore its existing local credential`);
    const { code } = await request('/v1/invitations', { role: 'executor', access: { allDevices: false, targets: [], gateway: true } });
    paired = await request('/v1/pair', { code, name: spec.name, deviceId: spec.deviceId });
    await writeFile(`${dir}/identity.json`, JSON.stringify(paired), { mode: 0o600 });
  }
  await writeFile(`${dir}/node-token`, paired.token, { mode: 0o600 });
  const config = { deviceId: spec.deviceId, name: spec.name, dataDir: 'data', listen: '127.0.0.1:47832', hub: false, executor: true, hubUrl: hub, nodeTokenFile: 'node-token', allowedOrigins: [], projects: [{ id: 'risk', name: '独立开发工作区', path: root + '/workspace' }], engine: { executable: root + '/engine/package/vendor/'+(spec.vendor??'x86_64-unknown-linux-musl')+'/bin/codex', home: root + '/engine-home', ...(!spec.direct?{useHubGateway:true}:{}) } };
  await writeFile(`${dir}/device.json`, JSON.stringify(config, null, 2), { mode: 0o600 });
  console.log(JSON.stringify({ alias: spec.alias, deviceId: spec.deviceId, remoteDirectory: root, modelCredentialsRead: false }));
}

// Update the earlier local peer to the same per-device gateway identity protocol.
if (only) process.exit(0);
const peer = JSON.parse(await readFile('.local/peer-device.json', 'utf8'));
const identity = identities.find((id) => id.role === 'executor' && id.deviceId === peer.deviceId);
if (identity) await request(`/v1/identities/${identity.id}`, { ...identity.access, gateway: true }, { method: 'PUT' });
peer.engine.proxyTokenFile = 'peer-token'; peer.engine.proxyUsername = peer.deviceId;
await writeFile('.local/peer-device.json', JSON.stringify(peer, null, 2), { mode: 0o600 });
