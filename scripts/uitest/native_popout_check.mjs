// Real Windows HWND/WebView2 smoke test. No browser emulation or runtime dependencies.
// Run through windows-window.sh against a fresh -root instance and local fixtures.
import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const [port,url,fx,out]=process.argv.slice(2);
if(process.platform!=='win32')throw new Error('This check requires real Windows/WebView2');
fs.mkdirSync(out,{recursive:true});
const H={'Content-Type':'application/json','X-Pullwell':'1'};
const results=[],errors=[],connections=[],ids=[];
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const check=(name,ok,extra)=>{results.push({name,pass:!!ok,extra});console.log(`${ok?'PASS':'FAIL'} ${name}${extra?' ('+extra+')':''}`);if(!ok)throw new Error(name);};
const until=async fn=>{for(let i=0;i<100;i++){if(await fn())return true;await sleep(150);}return false;};
const call=async body=>{const r=await fetch(url+'api/downloads',{method:'POST',headers:H,body:JSON.stringify(body)});if(!r.ok)throw new Error(await r.text());return r.json();};
const data=async()=>{const r=await fetch(url+'api/downloads');if(!r.ok)throw new Error('Download API unavailable');return r.json();};
const item=async id=>(await data()).items.find(v=>v.id===id);
const targets=async()=>(await(await fetch(`http://127.0.0.1:${port}/json/list`)).json()).filter(t=>t.type==='page');
const forId=async id=>(await targets()).filter(t=>{try{return new URL(t.url).searchParams.get('progress')===id;}catch{return false;}});
const windows=(name='',action='inspect',main=false)=>JSON.parse(execFileSync('powershell.exe',['-NoProfile','-ExecutionPolicy','Bypass','-File',fileURLToPath(new URL('native-window.ps1',import.meta.url)),'-ClassPrefix',main?'PullwellMain-':'PullwellProgress-','-TitleContains',name,'-Action',action],{encoding:'utf8',timeout:15000}).replace(/^\uFEFF/,''));
async function connect(target){
 const ws=new WebSocket(target.webSocketDebuggerUrl),pending=new Map();let serial=0;
 await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject;});
 ws.onmessage=event=>{const message=JSON.parse(event.data);if(message.id&&pending.has(message.id)){const entry=pending.get(message.id);pending.delete(message.id);clearTimeout(entry.timer);if(message.error)entry.reject(new Error(JSON.stringify(message.error)));else entry.resolve(message.result);}
  if(message.method==='Runtime.exceptionThrown')errors.push(message.params.exceptionDetails.exception?.description||message.params.exceptionDetails.text);
 };
 ws.onclose=()=>{for(const entry of pending.values()){clearTimeout(entry.timer);entry.reject(new Error('WebView closed'));}pending.clear();};
 const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++serial;const timer=setTimeout(()=>{pending.delete(id);reject(new Error('CDP timeout: '+method));},15000);pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method,params}));});
 const js=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true,userGesture:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);return r.result?.value;};
 const click=async(selector,closing=false)=>{const point=await js(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});if(!e)return null;e.scrollIntoView({block:'center'});const r=e.getBoundingClientRect();return{x:r.left+r.width/2,y:r.top+r.height/2};})()`);if(!point)throw new Error('Missing control '+selector);await send('Input.dispatchMouseEvent',{type:'mousePressed',...point,button:'left',clickCount:1});try{await send('Input.dispatchMouseEvent',{type:'mouseReleased',...point,button:'left',clickCount:1});}catch(error){if(!closing||error.message!=='WebView closed')throw error;}};
 const shot=async name=>{const r=await send('Page.captureScreenshot',{format:'png'});fs.writeFileSync(`${out}/${name}.png`,Buffer.from(r.data,'base64'));};
 const connection={js,click,shot,ws};connections.push(connection);await send('Runtime.enable');await send('Page.enable');return connection;
}
try{
 const target=(await targets()).find(t=>t.url===url);if(!target)throw new Error('Main WebView target missing');
 const main=await connect(target);
 check('native checks use the real WebView2 host',await main.js(`APP.host==='webview2'`));
 await main.js(`store.set('lang','en');store.set('zoom',1);applyZoom();`);
 for(let i=0;i<2;i++){const r=await call({action:'add',url:`${fx}/slow/${70+i}MB?bps=250000`,name:`native-popout-${i+1}.zip`,start:false,force:true});ids.push(r.added[0].id);}
 check('main sees both native test downloads',await until(()=>main.js(`DL.items.some(v=>v.id===${JSON.stringify(ids[1])})`)));
 const open=async id=>{await main.js(`progressBox(${JSON.stringify(id)})`);await main.click('#pgPopOut');};
 await open(ids[0]);
 check('pop-out creates a separate WebView target',await until(async()=>(await forId(ids[0])).length===1));
 const first=await connect((await forId(ids[0]))[0]);
 check('native pop-out loads its focused progress view',await until(()=>first.js(`!!document.querySelector('#pgGrid')&&document.querySelector('#pgTitle').textContent==='native-popout-1.zip'`)));
 const firstWindows=windows('native-popout-1.zip');
 check('native pop-out has its own visible Windows HWND',firstWindows.length===1&&firstWindows[0].Visible);
 check('main remains usable after native open',await main.js(`!document.querySelector('#dlDlg')&&!document.querySelector('#search').disabled`));
 await first.click('#pgMain');
 check('native Resume updates its download',await until(async()=>(await item(ids[0])).done>0&&await first.js(`document.querySelector('#pgState').textContent==='Downloading'`)));
 await sleep(1100);await first.click('#pgMain');
 check('native Pause affects only its own download',await until(async()=>(await item(ids[0])).state==='paused'&&(await item(ids[1])).state==='paused'));
 await open(ids[0]);
 const reused=windows('native-popout-1.zip');
 check('reopening reuses the native window',reused.length===1&&reused[0].Handle===firstWindows[0].Handle&&(await forId(ids[0])).length===1);
 windows('native-popout-1.zip','minimize');
 check('test minimized the actual HWND',windows('native-popout-1.zip')[0]?.Iconic===true);
 await open(ids[0]);
 const restored=windows('native-popout-1.zip');
 check('reopening restores the minimized native window',restored.length===1&&!restored[0].Iconic&&restored[0].Handle===firstWindows[0].Handle&&(await forId(ids[0])).length===1);
 await first.shot('native-progress-one');
 await open(ids[1]);
 check('different downloads create independent native windows',await until(async()=>(await forId(ids[1])).length===1));
 const second=await connect((await forId(ids[1]))[0]);
 check('second native view loads its own download',await until(()=>second.js(`document.querySelector('#pgTitle')?.textContent==='native-popout-2.zip'`)));
 const secondWindows=windows('native-popout-2.zip');
 check('native windows have different HWNDs',secondWindows.length===1&&secondWindows[0].Handle!==firstWindows[0].Handle);
 await second.click('#pgMain');
 check('second native download runs independently',await until(async()=>(await item(ids[1])).done>0&&(await item(ids[0])).state==='paused'));
 await first.click('#pgProps');await call({action:'remove',id:ids[0]});
 check('removal dismisses native Properties and clears details',await until(()=>first.js(`!document.querySelector('#dlDlg')&&document.querySelector('#pgUrl').textContent===''&&document.querySelector('#pgMain').hidden&&document.activeElement.id==='pgClose'`)));
 await second.shot('native-progress-two');
 const before=(await item(ids[1])).done;windows('','close',true);
 check('closing main hides only its Windows UI',await until(async()=>{const w=windows('','inspect',true);return w.length===1&&!w[0].Visible;}));
 check('native progress stays live after main closes',await until(async()=>(await item(ids[1])).done>before&&await second.js(`document.querySelector('#pgState').textContent==='Downloading'`)));
 await second.click('#pgClose',true);
 check('script Close destroys its native progress window',await until(async()=>(await forId(ids[1])).length===0&&windows('native-popout-2.zip').length===0));
 check('closing the native window leaves its download running',(await item(ids[1])).state==='downloading');
 await first.click('#progressClose',true);
 check('header Close destroys the remaining progress window',await until(async()=>(await forId(ids[0])).length===0));
 check('no native progress script errors',errors.length===0,errors.join('; '));
}catch(error){results.push({name:'native pop-out sequence',pass:false,error:error.stack});console.log('FAIL native pop-out sequence: '+error.stack);}
finally{
 for(const connection of connections)connection.ws.close();
 for(const id of ids){try{if(await item(id))await call({action:'remove',id,deleteFiles:true});}catch(error){errors.push('Cleanup: '+error.message);}}
 fs.writeFileSync(`${out}/results.json`,JSON.stringify({results,errors},null,2));
 console.log(`${results.filter(r=>r.pass).length}/${results.length} passed`);
 if(results.some(r=>!r.pass)||errors.length)process.exitCode=1;
}
