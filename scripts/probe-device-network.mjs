import { mkdir, writeFile } from 'node:fs/promises';
import { localHub } from './lib/local-hub.mjs';

const { request } = await localHub();
const expected = ['mewdex-risk-nest-1', 'mewdex-risk-nest-highspeed'];
const devices = await request('/v1/devices');
const gateway = await request('/v1/gateway');
const report = { at: new Date().toISOString(), devices: devices.map((d) => ({ id: d.id, name: d.name, platform: d.platform, online: d.online, projectIds: d.projects.map((p) => p.id) })), gateway, realRemoteNodesOnline: expected.every((id) => devices.some((d) => d.id === id && d.online)), inferenceRun: false };
await mkdir('.state/evidence', { recursive: true });
await writeFile('.state/evidence/real-device-network.json', JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
if (!report.realRemoteNodesOnline) process.exitCode = 1;
