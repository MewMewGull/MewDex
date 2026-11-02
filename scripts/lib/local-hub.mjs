import { readFile } from 'node:fs/promises';

export async function localHub() {
  const base = process.env.MEWDEX_PROBE_HUB ?? 'http://127.0.0.1:47830';
  const auth = JSON.parse(await readFile('.local/client-auth.json', 'utf8'));
  const request = async (path, body, options = {}) => {
    const response = await fetch(base + path, { method: options.method ?? (body ? 'POST' : 'GET'), headers: { Authorization: `Bearer ${options.token ?? auth.token}`, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
    const value = await response.json(); if (!response.ok) throw new Error(`${response.status}: ${value.error}`); return value;
  };
  return { base, request };
}
