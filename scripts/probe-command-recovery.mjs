// Run with: node --experimental-transform-types scripts/probe-command-recovery.mjs
// Read-only live Hub/SDK check. No engine command is sent or reconstructed.
import { readFile, writeFile } from 'node:fs/promises';
import { MewDexClient } from '../packages/sdk/src/index.ts';

const auth = JSON.parse(await readFile('.local/client-auth.json', 'utf8'));
const baseURL = process.env.MEWDEX_PROBE_HUB ?? 'http://127.0.0.1:47830';
const createInput = () => new MewDexClient({baseURL, token: auth.token});
const initialInput = createInput();
const devices = await initialInput.devices();
const metadata = new Set(['id','deviceId','sessionId','projectId','kind','state','createdAt']);
const checked = [];
for (const device of devices) {
  // A newly constructed input receives no locally remembered command IDs.
  const reopenedInput = createInput();
  let cursor = 0, pages = 0;
  const seen = new Set();
  while (true) {
    if (++pages > 100) throw new Error('Unexpected pending-receipt volume; no command was sent');
    const page = await reopenedInput.pendingCommands(device.id, cursor);
    for (const reference of page.commands) {
      if (seen.has(reference.id)) throw new Error('Duplicate recovered receipt');
      if (Object.keys(reference).some(key => !metadata.has(key))) throw new Error('Non-metadata field in receipt recovery');
      seen.add(reference.id);
      const observed = await reopenedInput.result(reference.id);
      if (observed.commandId !== reference.id) throw new Error('Receipt identifier changed');
    }
    if (!page.hasMore) break;
    if (page.after <= cursor) throw new Error('Recovery cursor did not advance');
    cursor = page.after;
  }
  checked.push({deviceId:device.id,pages,recovered:seen.size});
}
const report = {at:new Date().toISOString(),freshSDKInputPerDevice:true,existingReceiptsOnly:true,metadataOnly:true,devices:checked,modelTurnsSubmitted:0,inferenceRun:false,nativeAppRestartObserved:false};
await writeFile('.state/evidence/command-recovery.json', JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report));
