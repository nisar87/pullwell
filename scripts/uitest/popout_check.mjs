// Real per-download popup integration against a disposable app and local fixture server.
// NODE_PATH=<test modules> PLAYWRIGHT_BROWSERS_PATH=<cache>
// node popout_check.mjs <app-url> <fixtures-url> <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
const require=createRequire(import.meta.url);
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const axe=require.resolve(process.env.AXE_MODULE||'axe-core/axe.min.js');
const [url='http://127.0.0.1:9197/',fixture='http://127.0.0.1:8765',out='/tmp/pullwell-popouts']=process.argv.slice(2);
fs.mkdirSync(out,{recursive:true});
const headers={'Content-Type':'application/json','X-Pullwell':'1'};
const get=()=>fetch(new URL('api/downloads',url)).then(r=>r.json());
const call=async body=>{const r=await fetch(new URL('api/downloads',url),{method:'POST',headers,body:JSON.stringify(body)});if(!r.ok)throw new Error(await r.text());return r.json();};
const initial=await get();if(!initial.config?.dir?.startsWith('/tmp/'))throw new Error('Use a disposable app with downloads under /tmp');
await call({action:'config',config:{...initial.config,dir:out+'/downloads',maxActive:4}});
const ids=[];
for(let i=0;i<2;i++){
 const r=await call({action:'add',url:`${fixture}/slow/${70+i}MB?bps=250000`,name:`popout-${i+1}.zip`,start:false,force:true});
 ids.push(r.added[0].id);
}
const browser=await chromium.launch({headless:true,args:['--no-sandbox']});
const context=await browser.newContext({viewport:{width:1280,height:800},reducedMotion:'reduce'});
const main=await context.newPage(),errors=[],results=[];
context.on('page',page=>page.on('pageerror',e=>errors.push(e.message)));
main.on('pageerror',e=>errors.push(e.message));
const check=async(name,fn)=>{try{if(!await fn())throw new Error('assertion returned false');results.push({name,pass:true});console.log('PASS '+name);return true;}catch(e){results.push({name,pass:false,error:e.message});console.log('FAIL '+name+': '+e.message);return false;}};
const wait=async(fn)=>{for(let i=0;i<60;i++){if(await fn())return true;await new Promise(r=>setTimeout(r,100));}return false;};
const item=async id=>(await get()).items.find(v=>v.id===id);
const accessible=async page=>{await page.addScriptTag({path:axe});return page.evaluate(async()=>{const r=await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}});if(r.violations.length)throw new Error(JSON.stringify(r.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))));return true;});};
await main.goto(url);await main.waitForFunction(()=>typeof DL!=='undefined'&&DL);
await main.evaluate(id=>progressBox(id),ids[0]);
if(await check('progress dialog offers a separate-window action',()=>main.locator('#pgPopOut').count().then(n=>n===1))){
 const firstPromise=context.waitForEvent('page');await main.click('#pgPopOut');const first=await firstPromise;
 await first.waitForSelector('#progressWindow #pgTitle');
 await check('popup is a separate page for the requested download',()=>first.evaluate(id=>new URL(location).searchParams.get('progress')===id,ids[0]));
 await check('main workspace stays usable after popping out',async()=>await main.locator('#dlDlg').count()===0&&await main.locator('#search').isEnabled());
 await check('popup contains only the focused progress workspace',()=>first.evaluate(()=>document.querySelector('#app').hidden&&!document.querySelector('#progressWindow').hidden&&document.querySelector('#pgTitle').textContent==='popout-1.zip'));
 await check('opening the same download reuses its popup',async()=>{
  const before=context.pages().length;await main.evaluate(id=>progressBox(id),ids[0]);await main.click('#pgPopOut');await main.waitForTimeout(150);return context.pages().length===before;
 });
 await check('native reopens reach the host for window restore',async()=>{
  const host=await context.newPage();await host.goto(url);await host.waitForFunction(()=>typeof DL!=='undefined'&&DL);
  const passed=await host.evaluate(id=>{
   const originalHost=APP.host,originalOpen=window.open,calls=[];let proxyFocus=0;
   try{APP.host='webview2';window.open=(...args)=>{calls.push(args);return{closed:false,focus(){proxyFocus++;}};};ProgressWindow.open(id);ProgressWindow.open(id);}
   finally{APP.host=originalHost;window.open=originalOpen;}
   return calls.length===2&&calls.every(args=>args[1]==='_blank'&&new URL(args[0]).searchParams.get('progress')===id)&&proxyFocus===0;
  },ids[0]);await host.close();return passed;
 });
 await main.evaluate(id=>progressBox(id),ids[1]);const secondPromise=context.waitForEvent('page');await main.click('#pgPopOut');const second=await secondPromise;await second.waitForSelector('#progressWindow #pgTitle');
 await check('different downloads have independent progress windows',async()=>context.pages().length===3&&await second.locator('#pgTitle').innerText()==='popout-2.zip');
 let pendingTakes=0;for(const popup of [first,second])popup.on('request',request=>{if(request.method()==='POST'&&/api\/(app|downloads)$/.test(new URL(request.url()).pathname)){const b=request.postDataJSON();if(b?.take||b?.action==='takeAsk')pendingTakes++;}});
 await check('resume uses the existing download API',async()=>{await first.click('#pgMain');return wait(async()=>['downloading','done'].includes((await item(ids[0])).state));});
 await check('progress keeps updating with live transfer data',async()=>{const before=(await item(ids[0])).done;return wait(async()=>(await item(ids[0])).done>before&&await first.locator('#pgState').innerText()==='Downloading');});
 await check('popup title includes the download filename',()=>first.title().then(t=>t.includes('popout-1.zip')));
 await check('pause affects only its own download',async()=>{await first.waitForTimeout(1000);await first.click('#pgMain');return wait(async()=>(await item(ids[0])).state==='paused'&&(await item(ids[1])).state==='paused');});
 await check('properties closes back to the progress view',async()=>{await first.click('#pgProps');await first.locator('#dlDlg').waitFor();await first.keyboard.press('Escape');return await first.locator('#pgGrid').isVisible()&&await first.locator('#dlDlg').count()===0&&await first.evaluate(()=>document.activeElement.id==='pgProps');});
 for(const theme of ['light','dark']){
  await first.evaluate(theme=>{store.set('theme',theme);applyTheme();},theme);
  await first.setViewportSize({width:640,height:640});await first.screenshot({path:`${out}/${theme}.png`});
  await check(`${theme} popup passes accessibility checks`,()=>accessible(first));
  await check(`${theme} popup fits a short narrow window`,async()=>{await first.setViewportSize({width:420,height:380});await first.locator('#pgClose').scrollIntoViewIfNeeded();return first.locator('#pgClose').evaluate(el=>{const r=el.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight&&r.left>=0&&r.right<=innerWidth;});});
 }
 await first.setViewportSize({width:640,height:640});
 await check('popup ignores unrelated main-window shortcuts',async()=>{await first.locator('#pgClose').focus();await first.keyboard.press('Control+n');return await first.locator('#addUrl').count()===0;});
 await check('closing the main view leaves popouts live',async()=>{await main.close();await call({action:'resume',id:ids[1]});return wait(async()=>await second.locator('#pgState').innerText()==='Downloading');});
 await check('popouts do not consume pending add or notification requests',()=>pendingTakes===0);
 await check('closing a popout leaves its download running',async()=>{const closed=second.waitForEvent('close');await second.click('#pgClose');await closed;return (await item(ids[1])).state==='downloading';});
 await first.click('#pgProps');await first.locator('#dlDlg').waitFor();
 await check('removing the download dismisses its Properties dialog',async()=>{await call({action:'remove',id:ids[0]});return wait(async()=>await first.locator('#dlDlg').count()===0&&await first.evaluate(()=>document.activeElement.id==='pgClose'));});
 await check('removing the download clears stale controls and details',async()=>wait(async()=>await first.locator('#pgMain').isDisabled()&&await first.locator('#pgFolder').isDisabled()&&await first.locator('#pgProps').isDisabled()&&await first.locator('#pgUrl').innerText()===''&&await first.locator('#pgGrid').innerText()===''));
 const blocked=await context.newPage();await blocked.goto(url);await blocked.waitForFunction(()=>typeof DL!=='undefined'&&DL);
 await check('blocked popup keeps an actionable inline progress dialog',async()=>{await blocked.evaluate(id=>{window.open=()=>null;progressBox(id);},ids[1]);await blocked.click('#pgPopOut');return await blocked.locator('#dlDlg #pgGrid').isVisible()&&(await blocked.locator('#toast').innerText()).includes('window');});
 const unavailable=await context.newPage();await unavailable.goto(new URL('?progress=missing-download',url).href);
 await check('unknown download shows a useful empty progress state',async()=>{await unavailable.waitForSelector('#progressWindow');return wait(async()=>await unavailable.locator('#pgMain').isDisabled()&&/removed|available/.test(await unavailable.locator('#pgTitle').innerText()));});
 await check('removed-download state passes accessibility checks',()=>accessible(unavailable));
 await check('disconnected popup disables stale transfer controls',async()=>{
  await blocked.keyboard.press('Escape');
  await blocked.goto(new URL('?progress='+ids[1],url).href);await blocked.waitForSelector('#pgGrid');
  await blocked.route('**/api/downloads',route=>route.abort());
  return wait(async()=>await blocked.locator('#progressWindow .bn.bad').isVisible()&&await blocked.locator('#pgMain').isDisabled());
 });
 await check('popup recovers when the application reconnects',async()=>{await blocked.unroute('**/api/downloads');return wait(async()=>await blocked.locator('#progressWindow .bn.bad').count()===0&&await blocked.locator('#pgMain').isEnabled());});
 const completed=await call({action:'add',url:fixture+'/file/1MB',name:'finished-popup.pdf',force:true});ids.push(completed.added[0].id);
 await check('completed popup offers Open and Open folder',async()=>{
  if(!await wait(async()=>(await item(ids[2])).state==='done'))return false;
  await unavailable.goto(new URL('?progress='+ids[2],url).href);await unavailable.waitForSelector('#pgGrid');
  return await unavailable.locator('#pgMain').innerText()==='Open'&&await unavailable.locator('#pgFolder').isEnabled()&&await unavailable.locator('#pgBar').getAttribute('aria-valuenow')==='100';
 });
 const noRange=await call({action:'add',url:fixture+'/norange/12MB?bps=80000',name:'stream-popup.mp4',force:true});ids.push(noRange.added[0].id);
 await check('non-resumable pause confirmation returns to the same popout',async()=>{
  if(!await wait(async()=>{const v=await item(ids[3]);return v.noResume&&v.done>0;}))return false;
  await unavailable.goto(new URL('?progress='+ids[3],url).href);await unavailable.waitForSelector('#pgGrid');await unavailable.click('#pgMain');await unavailable.locator('#askNo').waitFor();await unavailable.click('#askNo');
  return await unavailable.locator('#pgGrid').isVisible()&&(await item(ids[3])).state==='downloading'&&await unavailable.locator('#dlDlg').count()===0;
 });
 await check('Urdu popout keeps the filename literal and fits large text',async()=>{
  await unavailable.evaluate(()=>{localStorage.setItem('pw.lang',JSON.stringify('ur'));store.set('zoom',1.3);});await unavailable.reload();await unavailable.waitForSelector('#pgGrid');await unavailable.setViewportSize({width:420,height:500});
  const rtl=await unavailable.locator('html').getAttribute('dir');const title=await unavailable.locator('#pgTitle').innerText();await unavailable.locator('#pgClose').scrollIntoViewIfNeeded();
  await unavailable.screenshot({path:out+'/urdu.png'});
  return rtl==='rtl'&&title==='stream-popup.mp4'&&await unavailable.locator('.progress-window-bar h1').innerText()==='ڈاؤن لوڈ کی پیش رفت'&&await unavailable.locator('#pgClose').evaluate(el=>{const r=el.getBoundingClientRect();return r.right<=innerWidth&&r.left>=0&&r.bottom<=innerHeight;});
 });
 await check('Urdu removed-download message is localized',async()=>{
  await unavailable.goto(new URL('?progress=missing-download',url).href);await unavailable.waitForSelector('#pgGrid');
  return unavailable.evaluate(()=>document.querySelector('#pgTitle').textContent===I18N.t('This download was removed')&&I18N.t('This download was removed')!=='This download was removed');
 });
}
await check('no popup runtime errors',()=>errors.length===0);
await browser.close();
for(const id of ids){const v=await item(id);if(v)await call({action:'remove',id,deleteFiles:true});}
fs.writeFileSync(out+'/results.json',JSON.stringify({results,errors},null,2));
console.log(`${results.filter(r=>r.pass).length}/${results.length} passed`);if(results.some(r=>!r.pass))process.exitCode=1;
