// Presentation and lifecycle for one download in an independent window.
// Transfer operations and rendering are shared with the inline progress dialog.
'use strict';
const ProgressWindow = (() => {
  const requested = new URLSearchParams(location.search).get('progress');
  const id = requested && /^[A-Za-z0-9_-]{1,128}$/.test(requested) ? requested : null;
  const windows = new Map();
  let ready = false;

  function open(downloadId){
    if (!DL?.items.some(v => v.id === downloadId)) return false;
    const native = APP.host === 'webview2';
    if (!native) {
      for (const [key, win] of windows) if (win.closed) windows.delete(key);
      const existing = windows.get(downloadId);
      if (existing) { existing.focus(); return true; }
    }
    const target = new URL('/', location.href);
    target.searchParams.set('progress', downloadId);
    let win;
    try { win = window.open(target.href, native ? '_blank' : 'pw-progress-' + downloadId, 'popup,width=660,height=650,resizable=yes,scrollbars=yes'); }
    catch (e) {}
    // Every native request reaches the host's per-ID focus/restore path.
    // A cached WindowProxy cannot restore a minimized Windows HWND.
    if (native) return true;
    if (win) { windows.set(downloadId, win); win.focus(); return true; }
    toast('The progress window was blocked. Allow pop-ups for this app, or use this progress view.', true);
    return false;
  }

  function init(){
    if (!id) return;
    document.body.classList.add('progress-mode');
    $('#app').hidden = true;
    const main = document.createElement('main');
    main.id = 'progressWindow'; main.tabIndex = -1;
    main.innerHTML = `<header class="progress-window-bar"><span class="progress-window-brand">${UI.icon('arrow')}<span data-nt>${esc(APP.name)}</span></span><h1>Download progress</h1><span class="sp"></span><button class="iconbtn" id="progressClose" aria-label="Close window" title="Close window">${UI.icon('close')}</button></header><div id="progressContent" aria-busy="true"><div class="loading-state" role="status"><span class="loading-mark" aria-hidden="true"></span><span>Getting your download ready…</span></div></div>`;
    document.body.insertBefore(main, $('#toast'));
    main.insertBefore($('#banner'), $('#progressContent'));
    $('#progressClose').onclick = close;
    window.addEventListener('storage', e => {
      if (e.key === 'pw.theme') applyTheme();
      if (e.key === 'pw.zoom') applyZoom();
      if (e.key === 'pw.lang') location.reload();
    });
  }

  function render(){
    if (!id) return false;
    if (!ready) {
      const content = $('#progressContent');
      content.innerHTML = progressMarkup(true);
      content.setAttribute('aria-busy', 'false');
      bindProgress(id, true); ready = true;
    }
    paintProgress();
    const v = DL.items.find(v => v.id === id);
    const status = v ? (v.state === 'done' ? I18N.t('Complete') : v.size > 0 ? Math.floor(pctOf(v)) + '%' : I18N.t(dlStateText(v))) : I18N.t('This download was removed');
    document.title = `${status} · ${v?.name || I18N.t('Download progress')} — ${APP.name}`;
    connection(true);
    return true;
  }

  function connection(online){
    if (!id) return;
    if (!online && ready) ['pgProps','pgFolder','pgMain'].forEach(key => $('#' + key).disabled = true);
  }

  function close(){
    window.close();
    // A directly visited browser tab may refuse script closing; return to the
    // library in that case. Native progress windows handle window.close().
    setTimeout(() => { if (!window.closed && APP.host !== 'webview2') location.replace('/'); }, 120);
  }

  return {id, open, init, render, connection, close};
})();
