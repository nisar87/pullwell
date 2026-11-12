/* Languages. The program's text is English in the source; this file translates what is on screen.
   A language file (i18n/<code>.js) calls I18N.add(code, {"English text": "translation", ...}).
   - exact text:        "Open folder": "Ordner öffnen"
   - text with values:  "{0} downloads added": "{0} Downloads hinzugefügt"   ({0}, {1} ... stand for any text, numbers, names)
   Text without a translation stays English, so a language can never leave a blank. Changing the language reloads the page.
   Language "xx" translates nothing and records every text it sees (used by the tests to find what is missing). */
(function () {
  'use strict';
  const NAMES = {en: 'English', es: 'Español', fr: 'Français', de: 'Deutsch', pt: 'Português (Brasil)', ru: 'Русский', tr: 'Türkçe',
    ar: 'العربية', ur: 'اردو', hi: 'हिन्दी', zh: '简体中文'};
  const SHIPPED = ['ur'];   // languages that have a file in i18n/ (written by scripts/i18n/build.mjs)
  const RTL = new Set(['ar', 'ur']);
  const dicts = {};            // code -> {exact: Map, pats: [{re, to, n}]}
  const ATTRS = ['title', 'placeholder', 'aria-label', 'alt'];
  const SKIP = new Set(['SCRIPT', 'STYLE', 'TEXTAREA', 'CODE', 'PRE', 'SVG', 'svg']);
  let lang = 'en', dict = null, rtl = false;
  const missing = new Set();
  const seenText = new WeakMap(), seenAttr = new WeakMap();

  const norm = s => s.replace(/\s+/g, ' ').trim();
  function add(code, map) {
    const d = dicts[code] || (dicts[code] = {exact: new Map(), pats: []});
    for (const k of Object.keys(map)) {
      const key = norm(k), v = map[k];
      if (!/\{\d+\}/.test(key)) { d.exact.set(key, v); continue; }
      if (/^(\{\d+\}[\s.,:;·\-–—/()%]*)+$/.test(key)) continue;     // a pattern that is only values would match everything
      const parts = key.split(/(\{\d+\})/);
      let src = '', n = 0;
      for (const p of parts) src += /^\{\d+\}$/.test(p) ? (n++, '([\\s\\S]*?)') : p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      d.pats.push({re: new RegExp('^' + src + '$'), to: v, n, len: key.length});
    }
    d.pats.sort((a, b) => b.len - a.len);
  }
  function tx(s) {
    if (!dict) return null;
    const k = norm(s);
    if (!k) return null;
    const r = tx1(k);
    if (r !== null || !k.includes(' · ')) return r;
    // "3 peers · Queue: Night": each part on its own; and a text of several sentences: each sentence on its own
    const join = (list, sep) => { let any = false; const parts = list.map(x => { const t = tx1(x); if (t !== null) any = true; return t === null ? x : t; }); return any ? parts.join(sep) : null; };
    if (k.includes(' · ')) { const r2 = join(k.split(' · '), ' · '); if (r2 !== null) return r2; }
    const sen = k.split(/(?<=[.?!])\s+/);
    return sen.length > 1 ? join(sen, ' ') : null;
  }
  function tx1(k) {
    const hit = dict.exact.get(k);
    if (hit !== undefined) return hit;
    for (const p of dict.pats) {
      const m = p.re.exec(k);
      if (m) return p.to.replace(/\{(\d+)\}/g, (_, i) => { const v = m[+i + 1]; if (v === undefined) return ''; const t = dict.exact.get(norm(v)); return t !== undefined ? t : isolate(v); });
    }
    return null;
  }
  // in a right-to-left sentence a file name or a path keeps its own direction
  const isolate = v => rtl && /[A-Za-z0-9\\/]/.test(v) ? '\u2066' + v + '\u2069' : v;
  const hasWords = s => /[A-Za-z]{3,}/.test(s);
  function note(s) { const k = norm(s); if (k && hasWords(k)) { missing.add(k); keep(); } }
  let keepT = 0;
  function recording() { try { return localStorage.getItem('pw.i18nrec') === '1'; } catch (e) { return false; } }
  function keep() {
    if (lang !== 'xx' && !recording()) return;
    clearTimeout(keepT);
    keepT = setTimeout(() => { try { const all = new Set(JSON.parse(localStorage.getItem('pw.i18nmiss') || '[]')); missing.forEach(x => all.add(x)); localStorage.setItem('pw.i18nmiss', JSON.stringify([...all])); } catch (e) {} }, 300);
  }

  function doText(n) {
    if (n.parentElement && n.parentElement.closest('[data-nt]')) return;
    const cur = n.data, was = seenText.get(n);
    if (was !== undefined && cur === was) return;
    const lead = cur.match(/^\s*/)[0], trail = cur.match(/\s*$/)[0];
    if (!cur.trim()) return;
    const t = tx(cur);
    if (t == null) { if (hasWords(cur)) note(cur); return; }
    const out = lead + t + trail;
    seenText.set(n, out); if (out !== cur) n.data = out;
  }
  function doAttr(el, a) {
    const cur = el.getAttribute(a);
    if (cur == null || !cur.trim()) return;
    let m = seenAttr.get(el); if (!m) seenAttr.set(el, m = {});
    if (m[a] === cur) return;
    const t = tx(cur);
    if (t == null) { if (hasWords(cur)) note(cur); return; }
    m[a] = t; if (t !== cur) el.setAttribute(a, t);
  }
  // a menu-bar word such as <u>T</u>asks: the whole word is translated and the underlined letter dropped
  function doAccel(el) {
    if (!el.querySelector(':scope > u') || [...el.childNodes].some(c => c.nodeType === 1 && c.nodeName !== 'U')) return false;
    const w = el.textContent, t = tx(w);
    if (t == null) { note(w); return true; }
    el.textContent = t; return true;
  }
  function walk(root) {
    if (root.nodeType === 3) { if (!(root.parentNode && SKIP.has(root.parentNode.nodeName))) doText(root); return; }
    if (root.nodeType !== 1 || SKIP.has(root.nodeName) || root.closest && root.closest('[data-nt]')) return;
    if (doAccel(root)) return;
    for (const a of ATTRS) if (root.hasAttribute(a)) doAttr(root, a);
    for (let c = root.firstChild; c; c = c.nextSibling) walk(c);
  }
  function start() {
    walk(document.documentElement);
    new MutationObserver(list => {
      for (const m of list) {
        if (m.type === 'characterData') { if (m.target.parentNode && !SKIP.has(m.target.parentNode.nodeName)) doText(m.target); }
        else if (m.type === 'attributes') { if (!(m.target.closest && m.target.closest('[data-nt]'))) doAttr(m.target, m.attributeName); }
        else m.addedNodes.forEach(n => walk(n));
      }
    }).observe(document.documentElement, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS});
  }

  function stored() { try { const v = JSON.parse(localStorage.getItem('pw.lang')); return typeof v === 'string' ? v : 'auto'; } catch (e) { return 'auto'; } }
  function auto() {
    for (const l of (navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || 'en'])) {
      const c = String(l).toLowerCase().split('-')[0];
      if (NAMES[c]) return c;
    }
    return 'en';
  }
  const pick = stored();
  lang = pick === 'auto' ? auto() : pick;
  if (lang !== 'xx' && !SHIPPED.includes(lang)) lang = 'en';
  document.documentElement.lang = lang === 'xx' ? 'en' : lang;
  if (lang !== 'en' && lang !== 'xx') document.write('<script src="i18n/' + lang + '.js"><\/script>');
  if (RTL.has(lang)) { rtl = true; document.documentElement.dir = 'rtl'; }

  window.I18N = {
    names: NAMES, choice: pick, lang, locale: pick === 'auto' || lang === 'xx' ? undefined : lang, shipped: SHIPPED,
    add,
    t(s) { const r = tx(s); return r == null ? s : r; },
    set(code) { try { localStorage.setItem('pw.lang', JSON.stringify(code)); } catch (e) {} location.reload(); },
    missing() { return [...missing]; },
    // only the languages whose file was loaded
    available() { return Object.keys(NAMES).filter(c => c === 'en' || dicts[c]); },
  };
  // the dictionary files are loaded by index.html before this runs to completion
  document.addEventListener('DOMContentLoaded', () => { dict = dicts[lang] || null; if (dict || lang === 'xx') start(); });
})();
