// Read-only verification of the approved HTTPS entry. No model command or pairing.
import { readFile, writeFile } from 'node:fs/promises';
import { MewDexClient } from '../packages/sdk/src/index.ts';

if (process.env.NODE_TLS_REJECT_UNAUTHORIZED === '0') throw new Error('TLS verification must be enabled for this probe');
const baseURL = 'https://mewdex.mewgullnest.top';
const healthResponse = await fetch(`${baseURL}/health`, {redirect:'error',signal:AbortSignal.timeout(12000)});
const health = await healthResponse.json();
if (!healthResponse.ok || health.name!=='MewDex' || health.protocolVersion!==1) throw new Error('Public endpoint is not the expected Hub');
const unauthenticated = await fetch(`${baseURL}/v1/me`, {redirect:'error',signal:AbortSignal.timeout(12000)});
if (unauthenticated.status!==401) throw new Error('Public endpoint did not enforce device authentication');
const auth=JSON.parse(await readFile('.local/client-auth.json','utf8'));
const local=new MewDexClient({baseURL:'http://127.0.0.1:47830',token:auth.token});
const client=new MewDexClient({baseURL,token:auth.token});
const [original,remote,devices,sessions]=await Promise.all([local.identity(),client.identity(),client.devices(),client.sessions()]);
if (original.id!==remote.id) throw new Error('Public route did not reach the existing Hub identity');
const report={at:new Date().toISOString(),baseURL,tlsVerified:true,healthStatus:healthResponse.status,unauthenticatedStatus:unauthenticated.status,sameHubIdentity:true,devices:devices.map(d=>({id:d.id,online:d.online})),activeSessions:sessions.filter(s=>['running','stopping','unknown'].includes(s.state)).map(s=>({id:s.id,deviceId:s.deviceId,state:s.state})),pairingCreated:false,modelTurnsSubmitted:0};
report.nativeOrigins=[];
for(const origin of ['tauri://localhost','http://tauri.localhost','https://tauri.localhost']) {
  const response=await fetch(`${baseURL}/v1/me`,{method:'OPTIONS',redirect:'error',headers:{Origin:origin,'Access-Control-Request-Method':'GET','Access-Control-Request-Headers':'authorization'},signal:AbortSignal.timeout(12000)});
  report.nativeOrigins.push({origin,status:response.status,allowed:response.ok&&response.headers.get('access-control-allow-origin')===origin});
}
if(process.argv.includes('--stream')) {
  const snapshot=await client.bootstrap();
  const controller=new AbortController();
  const timer=setTimeout(()=>controller.abort(),15000);
  let connected=false,cursorReceived=false;
  try {
    await client.subscribe({after:Math.max(0,snapshot.cursor-1),sessionId:'',signal:controller.signal,onState:state=>{if(state==='connected')connected=true;},onEvent:()=>{},onCursor:()=>{cursorReceived=true;controller.abort();}});
  } finally {clearTimeout(timer);controller.abort();}
  report.inputStream={connected,cursorReceived,protocol:'wss'};
  if(!connected||!cursorReceived)throw new Error('Public input event stream was not confirmed');
}
await writeFile('.state/evidence/public-ingress.json',JSON.stringify(report,null,2));
console.log(JSON.stringify(report));
if(report.nativeOrigins.some(origin=>!origin.allowed))throw new Error('A native input origin was rejected');
