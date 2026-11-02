// Development check of the real Hub -> Executor policy route; no turn is submitted.
import {readFile,writeFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {MewDexClient,commandID} from '../packages/sdk/src/index.ts';

const auth=JSON.parse(await readFile('.local/client-auth.json','utf8'));
const client=new MewDexClient({baseURL:'http://127.0.0.1:47830',token:auth.token});
const devices=await client.devices();
const target=devices.find(device=>device.id===(process.env.MEWDEX_PROBE_DEVICE??'8def8f91e1bb3fc777ae0af2269829c7'));
if(!target?.online || !target.projects.length)throw new Error('Expected development executor is offline');
const projectId=target.projects[0].id;
const original={mode:'focused',noTests:false,instructions:'MewDex 开发探测指令 A',completion:'仅检查策略版本，不提交模型回合'};
const created=await client.run({id:commandID(),deviceId:target.id,projectId,kind:'create',title:'策略版本开发探测',policy:original});
const first=created.session;if(!first)throw new Error('No session');
const changed=await client.savePolicy(first,{...original,mode:'edit',noTests:true,instructions:'MewDex 开发探测指令 B'});
let staleBlocked=false;
try {await client.savePolicy(first,original);}catch(error){if(error.state==='failed')staleBlocked=true;else throw error;}
const restored=await client.restorePolicy(changed,first.policyVersion);
const inspection=await client.inspectPolicy({deviceId:target.id,sessionId:first.id});
const versions=await client.policies(first.id);
const report={at:new Date().toISOString(),deviceId:target.id,sessionId:first.id,staleBlocked,versionCount:versions.length,exactRestored:restored.policyVersion===first.policyVersion&&restored.policyInstructions===first.policyInstructions&&inspection.developerInstructions===first.policyInstructions,sources:inspection.sources.map(source=>({kind:source.kind,label:source.label,textLength:source.text.length,textSHA256:createHash('sha256').update(source.text).digest('hex')})),executionTurnSubmitted:false,inferenceRun:false};
await writeFile(`.state/evidence/policies-${target.id}.json`,JSON.stringify(report,null,2));
console.log(JSON.stringify(report));
if(!report.staleBlocked||!report.exactRestored||report.versionCount<2)process.exitCode=1;
