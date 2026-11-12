// Accessible semantics and contrast across every shared frontend surface.
// NODE_PATH=<test node_modules> PLAYWRIGHT_BROWSERS_PATH=<browser cache>
// node a11y_check.mjs <app-url> <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const axe = require.resolve(process.env.AXE_MODULE || 'axe-core/axe.min.js');
const [url = 'http://127.0.0.1:9197/', out = '/tmp/pullwell-a11y'] = process.argv.slice(2);
fs.mkdirSync(out, {recursive:true});
const browser = await chromium.launch({args:['--no-sandbox']});
const page = await browser.newPage({viewport:{width:1280,height:800},reducedMotion:'reduce'});
const errors = [], results = [];
page.on('pageerror', e => errors.push(e.message));
await page.goto(url); await page.waitForFunction(() => typeof DL !== 'undefined' && DL);
// Synthetic presentation fixtures exercise all category colors without creating downloads.
await page.evaluate(() => {
  loading = true; view = {k:'all'}; q = '';
  DL.items = DL_CATS.map((category,i) => ({id:`a11y-${i}`,name:`${category.toLowerCase()}-sample.zip`,url:'https://files.example.test/sample.zip',category,state:i===1?'paused':i===2?'error':'done',size:10000000,done:i===1?2500000:10000000,order:i}));
  DL.config.queues = [{id:'a11y-queue',name:'Weekend',window:{enabled:false,from:'23:00',to:'06:00'},days:[],maxActive:0}];
  render();
});
const surfaces = [
  ['workspace','1'],['add','addDialog("")'],['general','showOptions("general")'],
  ['connection','showOptions("connection")'],['schedule','showOptions("schedule")'],
  ['browser','showOptions("browser")'],['torrents','showOptions("torrents")'],
  ['updates','showOptions("updates")'],['advanced','showOptions("advanced")'],
  ['queues','dlQueues()'],['folders','dlFolders()'],['grab','dlGrab()'],
  ['progress','progressBox("a11y-1")'],['properties','dlProps("a11y-1")'],
  ['shortcuts','shortcutsBox()'],['about','aboutBox()'],
  ['confirmation','void askBox({title:"Delete sample.zip?",text:"The download is removed from the list.",ok:"Delete",danger:true})'],
];
for (const theme of ['light','dark']) {
  await page.evaluate(theme => {store.set('theme',theme);applyTheme();},theme);
  for (const [surface,expression] of surfaces) {
    await page.evaluate(() => {if(document.querySelector('#dlDlg'))dlClose();});
    await page.evaluate(expression); await page.waitForTimeout(100);
    await page.addScriptTag({path:axe});
    const result = await page.evaluate(() => axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}));
    const violations = result.violations.map(v => ({id:v.id,impact:v.impact,nodes:v.nodes.map(n => ({target:n.target,summary:n.failureSummary}))}));
    results.push({theme,surface,violations});
    console.log(`${violations.length ? 'FAIL':'PASS'} ${theme} ${surface}${violations.length ? ': '+JSON.stringify(violations):''}`);
    if (['workspace','general','progress'].includes(surface)) await page.screenshot({path:`${out}/${theme}-${surface}.png`});
  }
}
await browser.close();
fs.writeFileSync(`${out}/results.json`,JSON.stringify({results,errors},null,2));
console.log(`${results.filter(r=>!r.violations.length).length}/${results.length} surfaces passed; ${errors.length} script errors`);
if(results.some(r=>r.violations.length)||errors.length)process.exitCode=1;
