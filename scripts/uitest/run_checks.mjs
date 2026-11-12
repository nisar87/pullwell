// Run the legacy browser suites with enforced failures and isolated fixture data.
// Usage: node run_checks.mjs <debug-port> <app-url> <fixtures-url> <out-dir> [suite ...]
// The app must already be a disposable test copy saving its downloads under /tmp.
import fs from 'node:fs';
import path from 'node:path';
import {spawn} from 'node:child_process';
const [port, url, fixtures, out, ...requested] = process.argv.slice(2);
if (!port || !url || !fixtures || !out) throw new Error('Expected debug port, app URL, fixtures URL, and output directory');
const suites = requested.length ? requested : ['ui', 'menu', 'prog', 'feat', 'flows', 'more', 'lang'];
const allowed = new Set(['ui', 'menu', 'prog', 'feat', 'flows', 'more', 'lang']);
if (suites.some(s => !allowed.has(s))) throw new Error('Unknown browser suite');
const headers = {'Content-Type':'application/json', 'X-Pullwell':'1'};
const endpoint = new URL('api/downloads', url);
const get = () => fetch(endpoint).then(r => r.json());
const call = body => fetch(endpoint, {method:'POST', headers, body:JSON.stringify(body)}).then(async r => {
  if (!r.ok) throw new Error(await r.text()); return r.json();
});
const initial = await get();
if (!initial.config?.dir?.startsWith('/tmp/')) throw new Error('Use an isolated test copy with its download folder under /tmp');
fs.mkdirSync(out, {recursive:true});
const results = [];
for (const suite of suites) {
  for (const item of (await get()).items || []) await call({action:'remove', id:item.id, deleteFiles:true});
  await call({action:'config', config:{...initial.config, dir:path.join(out, suite, 'downloads')}});
  // The feature suite verifies the default proxy choice before changing it.
  // Reset application preferences as well as download settings between suites.
  const appSettings = await fetch(new URL('api/app', url), {method:'POST', headers, body:JSON.stringify({proxy:{mode:'system'}})});
  if (!appSettings.ok) throw new Error('Could not reset isolated application proxy settings');
  const shots = path.join(out, suite); fs.mkdirSync(shots, {recursive:true});
  const log = await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [new URL(`${suite}_check.mjs`, import.meta.url).pathname, port, url, fixtures, shots], {stdio:['ignore','pipe','pipe']});
    let output = '', assertions = '';
    child.stdout.on('data', b => { output += b; assertions += b; }); child.stderr.on('data', b => output += b);
    child.on('error', reject); child.on('close', code => resolve({code, output, assertions}));
  });
  fs.writeFileSync(path.join(shots, 'check.log'), log.output);
  const failures = log.assertions.split('\n').filter(l => /^FAIL\b/.test(l) || /^script errors:\s*(?!none\b)\S/.test(l));
  const passes = log.assertions.split('\n').filter(l => /^PASS\b/.test(l)).length;
  const ok = log.code === 0 && failures.length === 0 && passes > 0;
  results.push({suite, ok, code:log.code, passes, failures});
  console.log(`${ok ? 'PASS' : 'FAIL'} ${suite}: ${passes} passing assertions${failures.length ? '\n' + failures.join('\n') : ''}`);
}
fs.writeFileSync(path.join(out, 'results.json'), JSON.stringify(results, null, 2));
if (results.some(r => !r.ok)) process.exitCode = 1;
