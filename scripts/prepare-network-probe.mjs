import { readFile, writeFile } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';

const auth = JSON.parse(await readFile('.local/client-auth.json', 'utf8'));
const config = JSON.parse(await readFile('.local/device.json', 'utf8'));
const base = 'http://127.0.0.1:47830';
async function post(path, body) {
  const response = await fetch(base + path, { method: 'POST', headers: { Authorization: `Bearer ${auth.token}`, 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const value = await response.json(); if (!response.ok) throw new Error(value.error); return value;
}
const { code } = await post('/v1/invitations', { role: 'executor',access:{allDevices:false,targets:[],gateway:true} });
const deviceId = 'probe-executor-' + randomBytes(8).toString('hex');
const paired = await post('/v1/pair', { code, name: '独立执行进程（开发风险验证）', deviceId });
await writeFile('.local/peer-token', paired.token, { mode: 0o600 });
config.gateway = { listen: '127.0.0.1:47831', allowedHosts: ['chatgpt.com', 'auth.openai.com', 'api.openai.com', 'chat.openai.com', 'ab.chatgpt.com'] };
await writeFile('.local/device.json', JSON.stringify(config, null, 2));
const peer = { ...config, deviceId, name: '独立执行进程（开发验证）', dataDir: 'peer-data', hub: false, gateway: undefined, hubUrl: base, nodeTokenFile: 'peer-token', engine: { ...config.engine, proxyUrl: 'http://127.0.0.1:47831', proxyTokenFile: 'peer-token',proxyUsername:deviceId } };
await writeFile('.local/peer-device.json', JSON.stringify(peer, null, 2), { mode: 0o600 });
console.log(JSON.stringify({ deviceId, hub: base, proxy: 'http://127.0.0.1:47831', credentialsPrinted: false }));
