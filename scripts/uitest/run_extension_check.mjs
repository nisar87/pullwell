// Portable wrapper for ext_check: serve its public-looking fixture host through
// a local test proxy, enforce assertions, and honor the offline-test stop signal.
// Run only against a disposable application whose download directory is under /tmp.
// NODE_PATH=<test node_modules> PLAYWRIGHT_BROWSERS_PATH=<cache>
// node run_extension_check.mjs <app-url> <work-dir> [fixtures-url]
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {spawn} from 'node:child_process';
const [url, work, fixtures = 'http://127.0.0.1:8765'] = process.argv.slice(2);
if (!url || !work) throw new Error('Expected isolated app URL and temporary work directory');
const headers = {'Content-Type':'application/json','X-Pullwell':'1'};
const config = await (await fetch(new URL('api/downloads',url))).json();
if (!config.config?.dir?.startsWith('/tmp/')) throw new Error('Use a disposable test copy saving files under /tmp');
fs.mkdirSync(work,{recursive:true});
// A documentation-only public IP avoids slow or unavailable DNS for the .test
// hostname. Both the browser and the unchanged app use the same fixture proxy.
const fixtureHost = '192.0.2.123';
const proxy = http.createServer((request,response) => {
  let target; try {target=new URL(request.url);} catch {response.writeHead(400).end();return;}
  if(target.hostname!==fixtureHost){response.writeHead(403).end('Test fixture host only');return;}
  const upstream=new URL(target.pathname+target.search,fixtures);
  const forwarded=http.request(upstream,{method:request.method,headers:{...request.headers,host:upstream.host}},remote=>{
    response.writeHead(remote.statusCode,remote.headers);remote.pipe(response);
  });
  forwarded.on('error',()=>response.writeHead(502).end());request.pipe(forwarded);
});
await new Promise(resolve=>proxy.listen(0,'127.0.0.1',resolve));
const proxyURL=`http://127.0.0.1:${proxy.address().port}`;
const settings=await fetch(new URL('api/app',url),{method:'POST',headers,body:JSON.stringify({proxy:{mode:'manual',addr:proxyURL}})});
if(!settings.ok)throw new Error('Could not configure the isolated fixture proxy');
await fetch(new URL('api/downloads',url),{method:'POST',headers,body:JSON.stringify({action:'extConfig',extMinMB:5,extOff:false,extNoAsk:false})});
const original=fs.readFileSync(new URL('ext_check.mjs',import.meta.url),'utf8');
const source=original.replaceAll('files.example.test',fixtureHost).replace('args: [',`args: [${JSON.stringify('--proxy-server='+proxyURL)}, `);
const script=path.join(work,'extension_check.mjs');fs.writeFileSync(script,source);
const run=path.join(work,'run');let output='',stopping=false;
const stop=async()=>{
  if(stopping)return;stopping=true;
  await fetch(new URL('api/app',url),{method:'POST',headers,body:JSON.stringify({quit:true})});
  for(let i=0;i<30;i++){
    await new Promise(resolve=>setTimeout(resolve,100));
    try{await fetch(new URL('ext/ping',url));}catch{break;}
  }
  fs.writeFileSync(path.join(run,'stopped'),'1');
};
const child=spawn(process.execPath,[script,url,run],{stdio:['ignore','pipe','pipe'],env:process.env});
child.stdout.on('data',b=>{output+=b;process.stdout.write(b);});
child.stderr.on('data',b=>{output+=b;if(String(b).includes('STOP_PROGRAM'))stop().catch(e=>console.error(e.message));});
const code=await new Promise((resolve,reject)=>{child.on('error',reject);child.on('close',resolve);});
proxy.closeAllConnections();proxy.close();
fs.writeFileSync(path.join(work,'check.log'),output);
if(code!==0||/^FAIL\b/m.test(output)||!/^\d+\/\d+ passed$/m.test(output))process.exitCode=1;
