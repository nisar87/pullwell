// Exercise popup presentation states inside a real extension page. Browser APIs
// are fixture-driven here; run_extension_check covers actual message integration.
// NODE_PATH=<test node_modules> PLAYWRIGHT_BROWSERS_PATH=<cache>
// node popup_check.mjs <unpacked-extension-dir> <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
const require=createRequire(import.meta.url);
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const axe=require.resolve(process.env.AXE_MODULE||'axe-core/axe.min.js');
const [extension,out]=process.argv.slice(2);
if(!extension||!out)throw new Error('Expected unpacked extension and output directory');
fs.mkdirSync(out,{recursive:true});
const context=await chromium.launchPersistentContext(`${out}/profile`,{headless:true,channel:'chromium',bypassCSP:true,args:['--no-sandbox',`--disable-extensions-except=${extension}`,`--load-extension=${extension}`]});
const worker=context.serviceWorkers()[0]||await context.waitForEvent('serviceworker');
const id=worker.url().split('/')[2];const results=[],errors=[];
for(const theme of ['light','dark'])for(const state of ['loading','down','off','pair','on','pair-error']){
  const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.emulateMedia({colorScheme:theme,reducedMotion:'reduce'});
  await page.addInitScript(state=>{
    const saved={token:state==='pair'||state==='pair-error'?null:'fixture-token',localOn:true,videoBtn:true,ignore:[],types:[]};
    window.browser={
      i18n:{getUILanguage:()=> 'en'},
      runtime:{sendMessage:(msg,reply)=>{
        if(state==='loading'&&msg.type==='ping')return;
        const result=msg.type==='ping'?(state==='down'?{ok:false}:state==='off'?{status:404}:{ok:true})
          :msg.type==='refresh'?{ok:true,data:{active:2,speed:2500000,enabled:true,minMB:5}}
          :msg.type==='pair'?{ok:false,error:'Could not connect'}:{list:[]};
        setTimeout(()=>reply(result),20);
      }},
      storage:{local:{get:async()=>saved,set:async v=>Object.assign(saved,v)}},
      permissions:{contains:async()=>true,request:async()=>true},
      tabs:{query:async()=>[{id:1,url:'https://files.example.test/'}]},
    };
  },state);
  await page.goto(`chrome-extension://${id}/popup.html`);await page.waitForTimeout(180);
  if(state==='pair-error'){await page.fill('#code','wrong-code');await page.click('#pair');await page.waitForTimeout(80);}
  const expected=state==='loading'?'v-loading':state==='pair-error'?'v-pair':`v-${state}`;
  const shown=await page.locator(`#${expected}`).isVisible();
  await page.addScriptTag({path:axe});
  const violations=await page.evaluate(async()=> (await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})));
  const errorOK=state!=='pair-error'||await page.locator('#perr').innerText()==='Could not connect';
  const ok=shown&&!violations.length&&errorOK;
  results.push({theme,state,ok,violations});console.log(`${ok?'PASS':'FAIL'} ${theme} popup ${state}${violations.length?': '+JSON.stringify(violations):''}`);
  await page.locator('body').screenshot({path:`${out}/${theme}-${state}.png`});await page.close();
}
const ur=await context.newPage();await ur.goto(`chrome-extension://${id}/popup.html?lang=ur`);await ur.waitForTimeout(250);
const rtl=await ur.evaluate(()=>document.documentElement.dir==='rtl'&&document.querySelector('.header-note').textContent!=='Downloads, handled.');
results.push({state:'urdu',ok:rtl});console.log(`${rtl?'PASS':'FAIL'} Urdu popup direction and new copy`);
await ur.locator('body').screenshot({path:`${out}/urdu.png`});
await context.close();fs.writeFileSync(`${out}/results.json`,JSON.stringify({results,errors},null,2));
if(results.some(r=>!r.ok)||errors.length)process.exitCode=1;
