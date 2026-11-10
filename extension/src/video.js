// {{NAME}}: a small "Download" button over videos, like the one IDM shows. It only exists on pages where the person
// allowed the extension for all sites (the extension adds this script itself then). It reads nothing from the page
// except where the video is and its link; the link goes to {{NAME}}, which shows its "Download file info" box.
(function () {
  if (window.__pullwellVideo) return;
  window.__pullwellVideo = true;
  var api = globalThis.browser || globalThis.chrome;
  var MIN_W = 200, MIN_H = 110;
  var host = null, btn = null, label = null, current = null, hideTimer = 0, videos = [];

  function build() {
    host = document.createElement('div');
    host.setAttribute('style', 'all:initial;position:fixed;z-index:2147483647;display:none;left:0;top:0');
    var root = host.attachShadow({ mode: 'open' });
    var css = document.createElement('style');
    css.textContent = '.b{all:initial;display:flex;align-items:center;gap:6px;height:28px;padding:0 11px 0 9px;border-radius:14px;cursor:pointer;' +
      'font:600 12.5px/1 "Segoe UI",system-ui,sans-serif;color:#fff;background:#0f8f86;box-shadow:0 2px 10px rgba(0,0,0,.4),0 0 0 1px rgba(255,255,255,.25) inset;white-space:nowrap}' +
      '.b:hover{background:#12a69b}.b:active{transform:translateY(1px)}.b.ok{background:#2f9e44}.b.bad{background:#c92a2a}' +
      'svg{width:14px;height:14px;flex:none}';
    btn = document.createElement('button');
    btn.className = 'b'; btn.type = 'button';
    btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v11M7 11l5 5 5-5M5 20h14"/></svg>';
    label = document.createElement('span');
    label.textContent = 'Download with {{NAME}}';
    btn.appendChild(label);
    root.appendChild(css); root.appendChild(btn);
    btn.addEventListener('mouseenter', function () { clearTimeout(hideTimer); });
    btn.addEventListener('click', function (e) {
      e.preventDefault(); e.stopPropagation();
      var v = current; if (!v) return;
      var src = v.currentSrc || v.src || '';
      if (!src) { var s = v.querySelector('source[src]'); if (s) src = s.src; }
      api.runtime.sendMessage({ type: 'grab', src: src, frame: location.href }, function (r) {
        var ok = r && r.ok;
        btn.className = 'b ' + (ok ? 'ok' : 'bad');
        label.textContent = ok ? (r.duplicate ? 'Already in {{NAME}}' : 'Sent to {{NAME}}') : (r && r.none ? 'No video link found' : '{{NAME}} is not running');
        setTimeout(function () { btn.className = 'b'; label.textContent = 'Download with {{NAME}}'; }, 2200);
      });
    }, true);
    document.documentElement.appendChild(host);
  }

  function rescan() { videos = Array.prototype.slice.call(document.querySelectorAll('video')); }
  function visible(v, r) {
    if (r.width < MIN_W || r.height < MIN_H) return false;
    if (r.bottom < 0 || r.right < 0 || r.top > innerHeight || r.left > innerWidth) return false;
    var cs = getComputedStyle(v);
    return cs.visibility !== 'hidden' && cs.display !== 'none';
  }
  function place(v, r) {
    if (!host) build();
    current = v;
    var w = btn.offsetWidth || 190;
    host.style.display = 'block';
    host.style.left = Math.max(4, Math.min(innerWidth - w - 4, r.right - w - 10)) + 'px';
    host.style.top = Math.max(4, r.top + 10) + 'px';
    clearTimeout(hideTimer);
    hideTimer = setTimeout(hide, 2600);
  }
  function hide() { if (host) host.style.display = 'none'; current = null; }

  var last = 0;
  document.addEventListener('mousemove', function (e) {
    var now = Date.now(); if (now - last < 120) return; last = now;
    if (!videos.length) return;
    for (var i = 0; i < videos.length; i++) {
      var v = videos[i], r = v.getBoundingClientRect();
      if (e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom && visible(v, r)) { place(v, r); return; }
    }
  }, true);
  document.addEventListener('scroll', hide, true);
  rescan();
  setInterval(rescan, 1500);
  try { new MutationObserver(rescan).observe(document.documentElement, { childList: true, subtree: true }); } catch (e) { }
})();
