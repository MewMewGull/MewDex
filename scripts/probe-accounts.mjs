// Development route probe. No user login, credential import, or inference.
import { readFile, writeFile } from 'node:fs/promises';
import { MewDexClient, commandID } from '../packages/sdk/src/index.ts';

const auth=JSON.parse(await readFile('.local/client-auth.json','utf8'));
const client=new MewDexClient({baseURL:'http://127.0.0.1:47830',token:auth.token});
const expected=['mewdex-risk-nest-1','mewdex-risk-nest-highspeed'];
const devices=await client.devices();
const report={at:new Date().toISOString(),devices:[],inferenceRun:false,accountAuthorized:false};
for(const deviceId of expected) {
  if(!devices.some(d=>d.id===deviceId&&d.online))throw new Error('Expected real executor is offline');
  const result=await client.run({id:commandID(),deviceId,kind:'account.read'});
  const account=result.data;
  const probe={deviceId,account};
  report.devices.push(probe);
  if(process.argv.includes('--device-code') && account.loginSupported && !account.signedIn) {
    const login=await client.run({id:commandID(),deviceId,kind:'account.login'});
    const pending=login.data;
    probe.deviceCode={started:true,verificationHost:new URL(pending.verificationUrl).hostname,codePresent:typeof pending.userCode==='string' && pending.userCode.length>0};
    // Never print or persist the one-time code. Immediately cancel this probe.
    const cancelled=await client.run({id:commandID(),deviceId,kind:'account.cancel',requestId:pending.loginId});
    probe.deviceCode.cancelled=cancelled.state==='completed';
  }
}
await writeFile('.state/evidence/remote-accounts.json',JSON.stringify(report,null,2));
console.log(JSON.stringify(report));
