// Download manager: the extra dialogs (add with options, page link grabber, properties, queues, folders, import/export).
// Loaded before app.js, which provides DL, dlAct, dlDialog, dlClose and the small helpers.

const DL_TYPES = {'All files':[], 'Videos':['mp4','mkv','avi','mov','webm','m4v','wmv','flv','ts'], 'Music':['mp3','flac','wav','aac','ogg','m4a','opus'], 'Archives':['zip','rar','7z','tar','gz','tgz','bz2','xz'], 'Programs':['exe','msi','dmg','iso','apk','deb','rpm'], 'Documents':['pdf','doc','docx','xls','xlsx','ppt','pptx','txt','epub','csv'], 'Images':['jpg','jpeg','png','gif','webp','bmp','svg','tif'], 'Torrents':['torrent','magnet']};
const DL_DAYS = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];
let dlSort = 'default', dlQf = '';
const dlQueueName = id => { const q = ((DL && DL.config.queues) || []).find(x => x.id === id); return q ? q.name : 'Main queue'; };
const dlQueueOpts = (cur, withMain = true) => (withMain ? [`<option value=""${!cur ? ' selected' : ''}>Main queue</option>`] : []).concat(((DL && DL.config.queues) || []).map(q => `<option value="${esc(q.id)}"${q.id === cur ? ' selected' : ''}>${esc(q.name)}</option>`)).join('');
const dlRowsOf = text => text.split(/\s*\n\s*/).map(s => s.trim()).filter(Boolean);

function dlMoreSettings(cfg){
  return `
    <div class="dnsrow"><div><b>If a file with that name is already there</b><div class="d">Keep both (adds a number), replace the old file, or skip the new download.</div></div><select class="sel" data-cfg="onExists" style="min-width:210px" aria-label="If the file exists">${[['rename','Keep both'],['overwrite','Replace the old file'],['skip','Skip the download']].map(o => `<option value="${o[0]}"${(cfg.onExists||'rename')===o[0]?' selected':''}>${o[1]}</option>`).join('')}</select></div>
    <div class="dnsrow"><div><b>Folders for types and websites</b><div class="d">Send Videos to one folder, Music to another, or everything from one website into its own folder.</div></div><button class="btn" data-dm="folders">Choose folders</button></div>
    <div class="dnsrow"><div><b>Queues</b><div class="d">Groups of downloads with their own hours and days, for example “Night” (23:00 to 06:00) or “Weekend”. ${(cfg.queues||[]).length} queue${(cfg.queues||[]).length===1?'':'s'}.</div></div><button class="btn" data-dm="queues">Manage queues</button></div>
    <div class="dnsrow"><div><b>Tell me when a download finishes</b><div class="d">A notification from ${esc(APP.name)}.</div></div><button class="tog${cfg.noNotify?'':' on'}" data-dm="notifyTog" role="switch" aria-checked="${!cfg.noNotify}" aria-label="Notification when finished"></button></div>
    <div class="dnsrow"><div><b>Run a program after each download <em class="rec" style="font-style:normal">Optional</em></b><div class="d">For example a virus scanner. Write the program and use <code>%f</code> for the file: <code>"C:\\Tools\\scan.exe" "%f"</code>. Leave empty for nothing.</div></div><div class="row" style="gap:8px"><input class="inp" id="dlAfterCmd" value="${esc(cfg.afterCmd||'')}" style="min-width:240px" aria-label="Program to run after a download" spellcheck="false"><button class="btn" data-dm="afterSave">Save</button></div></div>
    <div class="dnsrow"><div><b>Torrents and magnet links</b><div class="d">Turn off to refuse magnet links and .torrent files.</div></div><button class="tog${cfg.noTorrents?'':' on'}" data-dm="torTog" role="switch" aria-checked="${!cfg.noTorrents}" aria-label="Allow torrents"></button></div>
    ${cfg.noTorrents ? '' : `<div class="dnsrow"><div><b>Share while downloading</b><div class="d">Torrents work by taking turns: while a file comes in, the pieces you already have go out to others. Turn off to only download.</div></div><button class="tog${cfg.noUpload?'':' on'}" data-dm="upTog" role="switch" aria-checked="${!cfg.noUpload}" aria-label="Share while downloading"></button></div>
    ${cfg.noUpload ? '' : `<div class="dnsrow"><div><b>Upload speed limit</b><div class="d">For torrents only.</div></div>${limSel('uploadLimit', cfg.uploadLimit||0)}</div>`}`}
    <div class="dnsrow"><div><b>ffmpeg (joins picture and sound of web videos)</b><div class="d">${DL.ffmpeg ? 'Found. Videos with a separate sound track are joined into one file.' : 'Not found. Videos with a separate sound track are saved as two files. Put ffmpeg next to ' + esc(APP.name) + ' (or in its tools folder) to get one file.'}</div></div><span class="hint2">${DL.ffmpeg ? '✓ ready' : 'not installed'}</span></div>
    <div class="dnsrow"><div><b>Your list</b><div class="d">Save the list of downloads to a file, or load one (from this app, or a text file with one link per line).</div></div><div class="row" style="gap:8px"><button class="btn" data-dm="export">Save list</button><button class="btn" data-dm="import">Load list</button></div></div>`;
}

function dlToolsRow(){
  return `<div class="row" style="gap:8px;flex-wrap:wrap;margin-top:10px"><button class="btn" data-dm="options">Add with options…</button><button class="btn" data-dm="grab">Find links on a web page…</button><button class="btn" data-dm="queues">Queues</button></div>
  <div class="hint2" style="margin-top:8px">Tip: <code>photo[001-120].jpg</code> adds 120 links at once; <code>[a-f]</code> works for letters and <code>[1-100:5]</code> for every 5th.</div>`;
}

function dlSortSelect(){
  return `<select class="sel" id="dlSort" style="min-width:0" aria-label="Sort"><option value="default"${dlSort==='default'?' selected':''}>Sort: active first</option>${[['name','Name'],['size','Size (largest first)'],['added','Newest first'],['speed','Speed']].map(o => `<option value="${o[0]}"${dlSort===o[0]?' selected':''}>Sort: ${o[1]}</option>`).join('')}</select>`
    + ((DL.config.queues||[]).length ? `<select class="sel" id="dlQf" style="min-width:0" aria-label="Queue"><option value="">All queues</option><option value="main"${dlQf==='main'?' selected':''}>Main queue</option>${DL.config.queues.map(q => `<option value="${esc(q.id)}"${dlQf===q.id?' selected':''}>${esc(q.name)}</option>`).join('')}</select>` : '');
}

function dlSortItems(vis){
  const order = {downloading:0, queued:1, paused:2, error:3, done:4};
  const by = {
    default: (a,b) => (order[a.state] - order[b.state]) || ((b.finished||0) - (a.finished||0)) || a.order - b.order,
    name: (a,b) => (a.name||'').localeCompare(b.name||''),
    size: (a,b) => (b.size||0) - (a.size||0),
    added: (a,b) => (b.added||0) - (a.added||0),
    speed: (a,b) => (b.speed||0) - (a.speed||0),
  };
  return vis.sort(by[dlSort] || by.default);
}
function dlQueueMatch(v){ return !dlQf || (dlQf === 'main' ? !v.queue : v.queue === dlQf); }

function dlSegBar(v){
  if (!v.map || !v.map.length || v.state === 'done') return '';
  return `<div class="dlseg" title="Parts of the file already downloaded">${v.map.map(p => `<i style="opacity:${0.12 + p/100*0.88}"></i>`).join('')}</div>`;
}

/* ----- add with options ----- */
function dlOptions(){
  const cfg = DL.config;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add with options" style="max-width:600px;max-height:calc(90vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0">Add with options</h3>
    <div style="margin:12px 0;display:grid;gap:10px">
    <label class="hint2">Link (several: one per line; <code>[001-020]</code> makes a numbered series)<textarea class="inp" id="oUrl" rows="3" style="width:100%;margin-top:4px" spellcheck="false"></textarea></label>
    <label class="hint2">Save in (leave empty for the usual folder)<input class="inp" id="oDir" style="width:100%;margin-top:4px" placeholder="${esc(cfg.dir)}"></label>
    <div class="row" style="gap:10px;flex-wrap:wrap"><label class="hint2" style="flex:1;min-width:150px">User name (site that asks for a login)<input class="inp" id="oUser" autocomplete="off" style="width:100%;margin-top:4px"></label>
      <label class="hint2" style="flex:1;min-width:150px">Password<input class="inp" type="password" id="oPass" autocomplete="new-password" style="width:100%;margin-top:4px"></label></div>
    <label class="hint2">Other addresses of the same file (mirrors, one per line). They are tried if the first fails.<textarea class="inp" id="oMir" rows="2" style="width:100%;margin-top:4px" spellcheck="false"></textarea></label>
    <div class="row" style="gap:10px;flex-wrap:wrap"><label class="hint2" style="flex:1;min-width:150px">Queue<select class="sel" id="oQ" style="width:100%;margin-top:4px">${dlQueueOpts('')}</select></label>
      <label class="hint2" style="flex:1;min-width:150px">Connections<select class="sel" id="oConns" style="width:100%;margin-top:4px"><option value="0">Automatic</option>${[1,2,4,6,8,12,16].map(n => `<option>${n}</option>`).join('')}</select></label></div>
    <div class="row" style="gap:10px;flex-wrap:wrap"><label class="hint2" style="flex:1;min-width:150px">Checksum (optional)<input class="inp" id="oHash" style="width:100%;margin-top:4px" placeholder="SHA-256, SHA-1 or MD5"></label>
      <label class="hint2" style="flex:1;min-width:150px">Speed limit, KB/s (optional)<input class="inp" id="oLim" inputmode="numeric" style="width:100%;margin-top:4px" placeholder="No limit"></label></div>
    </div>
    <div class="note" style="margin-bottom:10px">A password is only used for this link and is not shown again.</div>
    <div class="row" style="justify-content:flex-end;gap:8px"><button class="btn" id="dlCancel">Cancel</button><button class="btn" id="oLater">Add, start later</button><button class="btn pri" id="oGo">Download now</button></div></div>`);
  $('#dlCancel').onclick = dlClose;
  const go1 = async start => {
    const urls = dlRowsOf($('#oUrl').value); if (!urls.length) return toast('Paste a link first', true);
    const body = {action:'add', urls, start, queue: $('#oQ').value, conns: +$('#oConns').value, force: true};
    const d = $('#oDir').value.trim(); if (d) body.dir = d;
    const u = $('#oUser').value; if (u) { body.user = u; body.pass = $('#oPass').value; }
    const mir = dlRowsOf($('#oMir').value); if (mir.length) body.mirrors = mir;
    const h = $('#oHash').value.trim(); if (h) body.hash = h;
    const lim = dlLimitInput($('#oLim').value); if (lim === null) return toast('Type the speed limit as a number of KB/s, or leave it empty', true);
    if (lim) body.limit = lim;
    const r = await dlAct(body, start ? 'Download started' : 'Added');
    if (r) dlClose();
  };
  $('#oGo').onclick = () => go1(true); $('#oLater').onclick = () => go1(false);
  setTimeout(() => $('#oUrl') && $('#oUrl').focus(), 30);
}

/* ----- find links on a web page ----- */
let dlGrabbed = [];
function dlGrab(){
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Find links on a page" style="max-width:720px;max-height:calc(90vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0">Find links on a web page</h3>
    <div class="hint2" style="margin:6px 0 12px">Type the address of a page. ${esc(APP.name)} lists the files it links to, and you choose which to download.</div>
    <div class="dlbox"><input class="inp" id="gUrl" placeholder="https://…" style="flex:1;min-width:240px" spellcheck="false" aria-label="Page address"><select class="sel" id="gType" style="min-width:0" aria-label="Kind of file">${Object.keys(DL_TYPES).map(k => `<option>${k}</option>`).join('')}</select><button class="btn pri" id="gFind">Find</button></div>
    <div id="gOut" style="margin-top:12px"></div>
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="dlCancel">Close</button><button class="btn pri" id="gAdd" disabled>Download selected</button></div></div>`);
  $('#dlCancel').onclick = dlClose;
  $('#gFind').onclick = async () => {
    const u = $('#gUrl').value.trim(); if (!u) return;
    $('#gOut').innerHTML = '<div class="hint2">Looking…</div>';
    try {
      const r = await api('downloads', {action:'grab', url: u, types: DL_TYPES[$('#gType').value]});
      dlGrabbed = r.links || [];
      $('#gOut').innerHTML = dlGrabbed.length ? `<div class="row" style="gap:10px;margin-bottom:6px"><label style="display:flex;gap:6px;align-items:center"><input type="checkbox" id="gAll" checked> All (${dlGrabbed.length})</label></div><div class="scrollbox" style="max-height:300px;overflow:auto">${dlGrabbed.map((g,i) => `<label style="display:flex;gap:8px;align-items:center;padding:3px 0"><input type="checkbox" class="gSel" data-i="${i}" checked><span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${esc(g.url)}">${esc(g.name)}</span></label>`).join('')}</div>` : '<div class="hint2">No matching links found on that page.</div>';
      $('#gAdd').disabled = !dlGrabbed.length;
      const all = $('#gAll'); if (all) all.onchange = () => $$('.gSel').forEach(c => c.checked = all.checked);
    } catch(e){ $('#gOut').innerHTML = `<div class="note bad">${esc(e.message)}</div>`; }
  };
  $('#gUrl').onkeydown = e => { if (e.key === 'Enter') $('#gFind').click(); };
  $('#gAdd').onclick = async () => {
    const urls = $$('.gSel').filter(c => c.checked).map(c => dlGrabbed[+c.dataset.i].url); if (!urls.length) return;
    const r = await dlAct({action:'add', urls}, null);
    if (r) { dlClose(); toast(`${(r.added||[]).length} downloads added` + (r.skipped ? `, ${r.skipped} already in your list` : '')); }
  };
  setTimeout(() => $('#gUrl') && $('#gUrl').focus(), 30);
}

/* ----- properties of one download ----- */
async function dlProps(id){
  const v0 = DL.items.find(x => x.id === id); if (!v0) return;
  let v = v0;
  if (v0.kind === 'torrent' && v0.nFiles) { const r = await dlAct({action:'files', id}); if (r && r.files) v = {...v0, files: r.files, select: r.select, trackers: r.trackers, priv: r.private, pieceLen: r.pieceLen}; }
  const cfg = DL.config, edit = v.state !== 'done';
  const when = t => t ? new Date(t*1000).toLocaleString(I18N.locale) : '-';
  const sh = dlLineShare(v);
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Properties" style="max-width:720px;max-height:calc(92vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0;word-break:break-all">${esc(v.name || 'Download')}</h3>
    <div class="hint2" style="margin:4px 0 10px;word-break:break-all">${esc(v.url)}</div>
    ${v.map && v.map.length && v.state !== 'done' ? `<div class="hint2">${v.kind === 'torrent' ? 'Pieces' : 'Parts of the file'}</div>${dlSegBar(v)}` : ''}
    <div style="display:grid;grid-template-columns:140px 1fr;gap:6px 12px;margin:12px 0;font-size:13.5px">
      <span class="hint2">State</span><span>${esc(dlStateText(v))}${v.err ? ' · ' + esc(v.err) : ''}</span>
      <span class="hint2">Size</span><span>${v.size > 0 ? bytes(v.size) : 'not known'}${v.done ? ' · ' + bytes(v.done) + ' downloaded' : ''}</span>
      <span class="hint2">Saved as</span><span style="word-break:break-all">${esc(v.path || '(not decided yet)')}</span>
      <span class="hint2">Added</span><span>${when(v.added)}</span>
      ${v.finished ? `<span class="hint2">Finished</span><span>${when(v.finished)}</span>` : ''}
      ${sh.sum > 0 ? `<span class="hint2">Carried by</span><span>${Object.entries(sh.tot).filter(([,b]) => b>0).map(([k,b]) => `${esc(labelOf(k))} ${Math.round(b/sh.sum*100)}%`).join(' · ')}</span>` : ''}
      ${v.kind === 'torrent' ? `<span class="hint2">Uploaded</span><span>${bytes(v.up||0)}${v.done > 0 ? ' · ratio ' + ((v.up||0)/v.done).toFixed(2) : ''}</span><span class="hint2">Peers</span><span>${v.peers||0} connected, ${v.seeds||0} seeding</span>${v.pieceLen ? `<span class="hint2">Pieces</span><span>${bytes(v.pieceLen)} each${v.priv ? ' · private torrent' : ''}</span>` : ''}${(v.trackers||[]).length ? `<span class="hint2">Trackers</span><span style="word-break:break-all">${v.trackers.slice(0,6).map(esc).join('<br>')}${v.trackers.length > 6 ? '<br>and ' + (v.trackers.length - 6) + ' more' : ''}</span>` : ''}` : ''}
      ${v.verify ? `<span class="hint2">Checksum</span><span>${v.verify === 'ok' ? '✓ matches' : v.verify === 'bad' ? '✗ does NOT match' : 'checking…'}</span>` : ''}
    </div>
    ${v.kind === 'torrent' && (v.files||[]).length ? `<h4 style="margin:10px 0 6px">Files in this torrent</h4><div class="scrollbox" style="max-height:220px;overflow:auto;font-size:13px">${v.files.map((f,i) => `<label style="display:flex;gap:8px;align-items:center;padding:3px 0"><input type="checkbox" class="tfSel" data-i="${i}"${!v.select || v.select[i] ? ' checked' : ''}${v.state === 'done' ? ' disabled' : ''}> <span style="flex:1;word-break:break-all">${esc(f.name)}</span><span class="hint2">${bytes(f.size)}</span></label>`).join('')}</div>${v.state === 'done' ? '' : '<div class="row" style="gap:8px;margin-top:6px"><button class="btn" id="tfSave">Download only the ticked files</button></div>'}` : ''}
    ${edit && v.kind === 'torrent' ? `<div style="display:grid;gap:10px;margin:8px 0"><label class="hint2">Queue<select class="sel" id="pQ" style="width:100%;margin-top:4px">${dlQueueOpts(v.queue)}</select></label><div class="row" style="gap:8px"><button class="btn" id="pSave">Save</button></div></div>` : ''}
    ${edit && v.kind !== 'torrent' ? `<div style="display:grid;gap:10px;margin:8px 0">
      <div class="row" style="gap:10px;flex-wrap:wrap"><label class="hint2" style="flex:1;min-width:160px">Queue<select class="sel" id="pQ" style="width:100%;margin-top:4px">${dlQueueOpts(v.queue)}</select></label>
        <label class="hint2" style="flex:1;min-width:160px">Connections<select class="sel" id="pC" style="width:100%;margin-top:4px"><option value="0">Automatic</option>${[1,2,4,6,8,12,16].map(n => `<option${v.conns===n?' selected':''}>${n}</option>`).join('')}</select></label></div>
      <label class="hint2">Speed limit for this download, in KB/s (empty = none)<input class="inp" id="pLim" inputmode="numeric" value="${v.limit > 0 ? Math.round(v.limit / 1024) : ''}" placeholder="No limit" style="width:100%;margin-top:4px"></label>
      <label class="hint2">Other addresses of the same file (mirrors, one per line)<textarea class="inp" id="pM" rows="2" style="width:100%;margin-top:4px" spellcheck="false">${esc((v.mirrors||[]).join('\n'))}</textarea></label>
      <div class="row" style="gap:8px"><button class="btn" id="pSave">Save these</button></div>
      <label class="hint2">The link stopped working? Paste a new link for the same file: what is already downloaded is kept. ${v.state === 'downloading' ? '<b>Pause the download first.</b>' : ''}<div class="row" style="gap:8px;margin-top:4px"><input class="inp" id="pLink" style="flex:1" placeholder="https://…" spellcheck="false"><button class="btn" id="pRelink"${v.state==='downloading'?' disabled':''}>Use this link</button></div></label>
    </div>` : ''}
    <h4 style="margin:14px 0 6px">What happened</h4>
    <div class="scrollbox" style="max-height:200px;overflow:auto;font-size:12.5px">${(v.log||[]).length ? (v.log||[]).slice().reverse().map(l => `<div style="padding:2px 0"><span class="hint2">${new Date(l.t*1000).toLocaleTimeString(I18N.locale)}</span> &nbsp;${esc(l.s)}</div>`).join('') : '<div class="hint2">Nothing yet.</div>'}</div>
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn pri" id="dlCancel">Close</button></div></div>`);
  $('#dlCancel').onclick = dlClose;
  if ($('#tfSave')) $('#tfSave').onclick = async () => { const sel = $$('.tfSel').map(b => b.checked); const r = await dlAct({action:'select', id, select: sel}, 'Saved'); if (r !== null) dlClose(); };
  if (!edit) return;
  if (v.kind === 'torrent') { $('#pSave').onclick = async () => { const r = await dlAct({action:'setQueue', id, queue: $('#pQ').value}, 'Saved'); if (r !== null) dlClose(); }; return; }
  $('#pSave').onclick = async () => {
    const r1 = await dlAct({action:'setQueue', id, queue: $('#pQ').value});
    const r2 = r1 !== null ? await dlAct({action:'setConns', id, conns: +$('#pC').value}) : null;
    const lim = dlLimitInput($('#pLim').value); if (lim === null) return toast('Type the speed limit as a number of KB/s, or leave it empty', true);
    const r2b = r2 !== null ? await dlAct({action:'limit', id, limitBytes: lim}) : null;
    const r3 = r2b !== null ? await dlAct({action:'setMirrors', id, urls: dlRowsOf($('#pM').value)}, 'Saved') : null;
    if (r3 !== null) dlClose();
  };
  $('#pRelink').onclick = async () => { const u = $('#pLink').value.trim(); if (!u) return; const r = await dlAct({action:'relink', id, url: u}, 'Link changed'); if (r !== null) dlClose(); };
}

/* ----- queues ----- */
function dlQueues(){
  const qs = JSON.parse(JSON.stringify(DL.config.queues || []));
  const draw = () => {
    dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Queues" style="max-width:720px;max-height:calc(92vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0">Queues</h3>
      <div class="hint2" style="margin:6px 0 12px">A queue is a group of downloads with its own hours. Downloads in no queue (the main queue) follow your general settings. Put a download in a queue from its Properties, or when you add it.</div>
      ${qs.length ? qs.map((q,i) => `<div class="card" style="margin:0 0 10px;padding:12px;background:var(--inset)">
        <div class="row" style="gap:8px;flex-wrap:wrap"><input class="inp qn" data-i="${i}" value="${esc(q.name)}" style="flex:1;min-width:150px" aria-label="Queue name" maxlength="40">
          <label class="hint2">At once <select class="sel qm" data-i="${i}" style="min-width:0">${[0,1,2,3,4,6,8,10].map(n => `<option value="${n}"${q.maxActive===n?' selected':''}>${n || 'no limit'}</option>`).join('')}</select></label>
          <button class="btn" data-q="del" data-i="${i}">Delete</button></div>
        <div class="row" style="gap:8px;flex-wrap:wrap;margin-top:8px"><label style="display:flex;gap:6px;align-items:center" class="hint2"><input type="checkbox" class="qw" data-i="${i}"${q.window.enabled?' checked':''}> Only between</label>
          <input type="time" class="inp qf" data-i="${i}" value="${esc(q.window.from)}" style="min-width:0"> <span class="hint2">and</span> <input type="time" class="inp qt" data-i="${i}" value="${esc(q.window.to)}" style="min-width:0"></div>
        <div class="row" style="gap:10px;flex-wrap:wrap;margin-top:8px"><span class="hint2">Days</span>${DL_DAYS.map((d,k) => `<label style="display:flex;gap:4px;align-items:center" class="hint2"><input type="checkbox" class="qd" data-i="${i}" data-k="${k}"${(q.days||[]).includes(k)?' checked':''}>${d}</label>`).join('')}<span class="hint2">(none ticked = every day)</span></div>
        <div class="row" style="gap:12px;margin-top:8px"><label style="display:flex;gap:6px;align-items:center" class="hint2"><input type="checkbox" class="qp" data-i="${i}"${q.paused?' checked':''}> Paused</label>
          <button class="btn" data-q="start" data-i="${i}" data-id="${esc(q.id)}">Resume all in this queue</button></div></div>`).join('') : '<div class="hint2">No queues yet.</div>'}
      <div class="row" style="gap:8px;margin-top:10px"><button class="btn" data-q="add"${qs.length>=12?' disabled':''}>Add a queue</button></div>
      <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="dlCancel">Cancel</button><button class="btn pri" id="qSave">Save</button></div></div>`);
    $('#dlCancel').onclick = dlClose;
    $('#qSave').onclick = async () => { sync(); const r = await dlAct({action:'config', config: {...DL.config, queues: qs}}, 'Queues saved'); if (r !== null) dlClose(); };
    $$('[data-q]').forEach(b => b.onclick = async () => {
      sync();
      if (b.dataset.q === 'add') qs.push({id:'', name:'Queue ' + (qs.length + 1), maxActive:0, window:{enabled:false, from:'23:00', to:'06:00'}, days:[], paused:false});
      else if (b.dataset.q === 'del') { const i = +b.dataset.i; if ((await askBox({title:`Delete the queue "${qs[i].name}"?`, text:'Its downloads move to the main queue. Nothing is deleted from your disk.', ok:'Delete queue'})).ok) qs.splice(i, 1); }
      else if (b.dataset.q === 'start') { await dlAct({action:'startQueue', queue: b.dataset.id}, 'Started'); return; }
      draw();
    });
  };
  const sync = () => {
    $$('.qn').forEach(e => qs[+e.dataset.i].name = e.value.trim() || 'Queue');
    $$('.qm').forEach(e => qs[+e.dataset.i].maxActive = +e.value);
    $$('.qw').forEach(e => qs[+e.dataset.i].window.enabled = e.checked);
    $$('.qf').forEach(e => qs[+e.dataset.i].window.from = e.value || '23:00');
    $$('.qt').forEach(e => qs[+e.dataset.i].window.to = e.value || '06:00');
    $$('.qp').forEach(e => qs[+e.dataset.i].paused = e.checked);
    qs.forEach(q => q.days = []); $$('.qd').forEach(e => { if (e.checked) qs[+e.dataset.i].days.push(+e.dataset.k); });
  };
  draw();
}

/* ----- folders by type and by website ----- */
function dlFolders(){
  const cfg = DL.config, rules = JSON.parse(JSON.stringify(cfg.siteRules || []));
  const draw = () => {
    dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Folders" style="max-width:720px;max-height:calc(92vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0">Folders for types and websites</h3>
      <div class="hint2" style="margin:6px 0 12px">A folder you choose while adding a download always wins. Then a website's folder, then the folder for the type, then your main folder (${esc(cfg.dir)}).</div>
      <h4 style="margin:6px 0">By type</h4>
      <div style="display:grid;gap:6px">${DL_CATS.map(c => `<label class="row" style="gap:10px"><span style="width:100px">${c}</span><input class="inp fc" data-c="${c}" value="${esc((cfg.catDirs||{})[c]||'')}" placeholder="(main folder / ${c})" style="flex:1" spellcheck="false"></label>`).join('')}</div>
      <h4 style="margin:14px 0 6px">By website</h4>
      <div style="display:grid;gap:6px">${rules.map((r,i) => `<div class="row" style="gap:8px"><input class="inp fs" data-i="${i}" value="${esc(r.site)}" placeholder="example.com" style="width:180px" spellcheck="false"><input class="inp fd" data-i="${i}" value="${esc(r.dir)}" placeholder="Folder" style="flex:1" spellcheck="false"><button class="btn" data-fr="${i}">Remove</button></div>`).join('') || '<div class="hint2">No websites yet.</div>'}</div>
      <div class="row" style="margin-top:8px"><button class="btn" id="fAdd">Add a website</button></div>
      <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="dlCancel">Cancel</button><button class="btn pri" id="fSave">Save</button></div></div>`);
    $('#dlCancel').onclick = dlClose;
    $('#fAdd').onclick = () => { sync(); rules.push({site:'', dir:''}); draw(); };
    $$('[data-fr]').forEach(b => b.onclick = () => { sync(); rules.splice(+b.dataset.fr, 1); draw(); });
    $('#fSave').onclick = async () => {
      sync(); const catDirs = {}; $$('.fc').forEach(e => { if (e.value.trim()) catDirs[e.dataset.c] = e.value.trim(); });
      const r = await dlAct({action:'config', config: {...DL.config, catDirs, siteRules: rules.filter(x => x.site.trim() && x.dir.trim())}}, 'Saved'); if (r !== null) dlClose();
    };
  };
  const sync = () => { $$('.fs').forEach(e => rules[+e.dataset.i].site = e.value); $$('.fd').forEach(e => rules[+e.dataset.i].dir = e.value); };
  draw();
}

/* ----- save / load the list ----- */
async function dlExport(){
  // the program writes the file (into the download folder) and shows it: the window cannot save files reliably
  try { const r = await api('app', {saveList: true}); toast('List saved: ' + r.path); } catch(e){ toast(e.message, true); }
}
function dlImport(){
  const f = document.createElement('input'); f.type = 'file'; f.accept = '.json,.txt,.lst,.csv';
  f.onchange = async () => {
    const file = f.files[0]; if (!file) return; if (file.size > 5e6) return toast('That file is too big', true);
    const r = await dlAct({action:'import', text: await file.text()});
    if (r) toast(!r.imported ? 'Everything in that list is already here' : `${r.imported} download${r.imported === 1 ? '' : 's'} added` + (r.skipped ? `, ${r.skipped} already here or not usable` : ''));
  };
  f.click();
}

document.addEventListener('click', async e => {
  const t = e.target.closest('button'); if (!t || !t.dataset.dm || !DL) return;
  const a = t.dataset.dm;
  if (a === 'options') return dlOptions();
  if (a === 'grab') return dlGrab();
  if (a === 'queues') return dlQueues();
  if (a === 'logins') return dlLogins();
  if (a === 'folders') return dlFolders();
  if (a === 'props') return dlProps(t.dataset.id);
  if (a === 'export') return dlExport();
  if (a === 'import') return dlImport();
  if (a === 'torTog') return void dlAct({action:'config', config: {...DL.config, noTorrents: !DL.config.noTorrents}});
  if (a === 'upTog') return void dlAct({action:'config', config: {...DL.config, noUpload: !DL.config.noUpload}});
  if (a === 'notifyTog') return void dlAct({action:'config', config: {...DL.config, noNotify: !DL.config.noNotify}});
  if (a === 'afterSave') {
    const v = $('#dlAfterCmd').value.trim();
    if (v && !(await askBox({title:'Run this after every finished download?', text: v + ' — only use a program you trust.', ok:'Save'})).ok) return showOptions('advanced');
    await dlAct({action:'config', config: {...DL.config, afterCmd: v}}, v ? 'Saved' : 'Cleared');
    if (v) showOptions('advanced');
    return;
  }
});
document.addEventListener('change', e => {
  if (e.target.id === 'dlSort') { dlSort = e.target.value; paintDL(); }
  if (e.target.id === 'dlQf') { dlQf = e.target.value; paintDL(); }
});


/* ----- a speed limit typed in KB/s: 0 = none, null = not a number ----- */
function dlLimitInput(text){
  const t = String(text || '').trim(); if (!t) return 0;
  if (!/^\d{1,7}$/.test(t)) return null;
  return +t * 1024;
}

/* ----- saved logins: one user name and password per website ----- */
async function dlLogins(){
  let list = [];
  try { list = (await api('downloads', {action:'loginList'})).logins || []; } catch(e){ return toast(e.message || 'That did not work', true); }
  const draw = () => {
    dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Saved logins" style="max-width:600px;max-height:calc(92vh / var(--zoom, 1));overflow:auto"><h3 style="margin:0">Saved logins</h3>
      <div class="hint2" style="margin:6px 0 12px">For a website that asks for a user name and password. It is used for every download from that website and its subdomains, and is never sent to another website. Passwords stay on this PC, protected by your Windows account, and are never shown again.</div>
      <div id="lgList">${list.length ? list.map((l, i) => `<div class="row" style="gap:8px;padding:6px 0;border-bottom:1px solid var(--line)"><b style="flex:1;word-break:break-all">${esc(l.host)}</b><span class="hint2">${esc(l.user)}</span><button class="btn" data-lg="del" data-i="${i}" aria-label="Remove the login for ${esc(l.host)}">Remove</button></div>`).join('') : '<div class="hint2">No logins saved yet.</div>'}</div>
      <h4 style="margin:14px 0 6px">Add or change a login</h4>
      <div style="display:grid;gap:10px">
        <label class="hint2">Website<input class="inp" id="lgHost" style="width:100%;margin-top:4px" placeholder="example.com" spellcheck="false" autocomplete="off"></label>
        <div class="row" style="gap:10px;flex-wrap:wrap"><label class="hint2" style="flex:1;min-width:150px">User name<input class="inp" id="lgUser" autocomplete="off" style="width:100%;margin-top:4px"></label>
          <label class="hint2" style="flex:1;min-width:150px">Password<input class="inp" type="password" id="lgPass" autocomplete="new-password" style="width:100%;margin-top:4px"></label></div>
      </div>
      <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="dlCancel">Close</button><button class="btn pri" id="lgSave">Save login</button></div></div>`);
    $('#dlCancel').onclick = dlClose;
    $('#lgSave').onclick = async () => {
      const host = $('#lgHost').value.trim(), user = $('#lgUser').value, pass = $('#lgPass').value;
      if (!host || !user) return toast('Type the website and the user name', true);
      try { list = (await api('downloads', {action:'loginSet', host, user, pass})).logins || []; toast('Login saved'); draw(); } catch(e){ toast(e.message || 'That did not work', true); }
    };
    $$('[data-lg="del"]').forEach(b => b.onclick = async () => {
      const l = list[+b.dataset.i];
      const ans = await askBox({title:`Remove the login for ${l.host}?`, text:'Downloads from that website will ask for a login again. Nothing already downloaded is touched.', ok:'Remove'});
      if (!ans.ok) return draw();
      try { list = (await api('downloads', {action:'loginRemove', host: l.host})).logins || []; toast('Login removed'); draw(); } catch(e){ toast(e.message || 'That did not work', true); }
    });
    const f = $('#lgHost'); if (f) f.focus();
  };
  draw();
}
