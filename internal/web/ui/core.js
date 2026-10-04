'use strict';
// Zapret Manager for Windows — panel core: API, navigation, jobs, dialogs, small UI helpers.

const ZM = {
  pages: {}, order: [], cur: null, timer: null, theme: 'auto', dash: null,

  page(id, def) { this.pages[id] = def; this.order.push(id); },

  async api(cmd, args) {
    let r;
    try {
      r = await fetch('/api/' + cmd, { method: 'POST', headers: { 'X-ZM': '1', 'Content-Type': 'application/json' }, body: JSON.stringify(args || {}) });
    } catch (e) { throw new Error('Служба Zapret Manager не отвечает'); }
    const j = await r.json().catch(() => ({ error: 'Неверный ответ сервера' }));
    if (j && j.auth) { document.getElementById('page').innerHTML = '<div class="zm-card"><h3>Нет доступа</h3><p>Откройте панель ярлыком «Zapret Manager».</p></div>'; throw new Error(j.error); }
    if (j && j.error) throw new Error(j.error);
    return j;
  },

  // call: API + toast on error; returns null on failure. Starts a job dialog if the API returned one.
  async call(cmd, args, opts) {
    opts = opts || {};
    try {
      const r = await this.api(cmd, args);
      if (r && r.job) { await this.jobDialog(r.job, opts.title || 'Выполняется операция'); }
      else if (opts.ok) this.toast(opts.ok, 'ok');
      if (opts.reload !== false) this.refresh();
      return r;
    } catch (e) { this.toast(e.message, 'err'); return null; }
  },

  toast(msg, kind) {
    const t = document.createElement('div');
    t.className = 'toast ' + (kind || '');
    t.textContent = msg;
    document.getElementById('toasts').appendChild(t);
    setTimeout(() => t.remove(), kind === 'err' ? 9000 : 3500);
  },

  modal(html) {
    const m = document.getElementById('modal');
    m.innerHTML = '<div class="zm-dialog">' + html + '</div>';
    m.hidden = false;
    return m.firstChild;
  },
  closeModal() { const m = document.getElementById('modal'); m.hidden = true; m.innerHTML = ''; },

  confirm(title, text, okLabel, danger) {
    return new Promise(res => {
      const d = this.modal(`<h3>${esc(title)}</h3><p class="muted">${text || ''}</p>
        <div class="zm-actions"><button class="btn" data-x="no">Отмена</button><button class="btn ${danger ? 'dng' : 'pri'}" data-x="yes">${esc(okLabel || 'Продолжить')}</button></div>`);
      d.addEventListener('click', e => { const x = e.target.dataset.x; if (x) { this.closeModal(); res(x === 'yes'); } });
    });
  },

  prompt(title, text, value, multiline) {
    return new Promise(res => {
      const d = this.modal(`<h3>${esc(title)}</h3><p class="muted">${text || ''}</p>
        ${multiline ? `<textarea class="mono" id="pv">${esc(value || '')}</textarea>` : `<input type="text" id="pv" style="width:100%" value="${esc(value || '')}">`}
        <div class="zm-actions"><button class="btn" data-x="no">Отмена</button><button class="btn pri" data-x="yes">OK</button></div>`);
      const inp = d.querySelector('#pv'); inp.focus();
      d.addEventListener('click', e => { const x = e.target.dataset.x; if (x) { const v = inp.value; this.closeModal(); res(x === 'yes' ? v : null); } });
    });
  },

  // jobDialog shows a live log of a background job until it ends.
  jobDialog(job, title) {
    return new Promise(res => {
      const d = this.modal(`<h3><span class="spin" id="jspin"></span> ${esc(title)}</h3><pre class="log" id="jlog"></pre>
        <div class="zm-actions"><button class="btn dng" data-x="cancel">Прервать</button><button class="btn pri" data-x="close" disabled>Закрыть</button></div>`);
      const log = d.querySelector('#jlog');
      let from = 0, done = false;
      d.addEventListener('click', async e => {
        const x = e.target.dataset.x;
        if (x === 'cancel' && !done) { await this.api('jobs_cancel', { job }).catch(() => {}); }
        if (x === 'close' && done) { this.closeModal(); res(); }
      });
      const tick = async () => {
        let r;
        try { r = await this.api('log_tail', { job, from }); } catch (e) { log.insertAdjacentHTML('beforeend', `<span class="e">${esc(e.message)}</span>\n`); }
        if (r) {
          for (const l of r.lines || []) log.insertAdjacentHTML('beforeend', fmtLog(l) + '\n');
          from += (r.lines || []).length;
          log.scrollTop = log.scrollHeight;
          if (!r.state.running) {
            done = true;
            d.querySelector('#jspin').remove();
            d.querySelector('[data-x=cancel]').disabled = true;
            const c = d.querySelector('[data-x=close]'); c.disabled = false; c.focus();
            log.insertAdjacentHTML('beforeend', r.state.failed ? `<span class="e">Операция завершилась с ошибкой</span>` : `<span class="k">Готово</span>`);
            log.scrollTop = log.scrollHeight;
            return;
          }
        }
        setTimeout(tick, 600);
      };
      tick();
    });
  },

  nav() {
    const groups = [
      ['', ['dash']], ['Обход блокировок', ['zapret', 'zapret2', 'bytetube']],
      ['VPN и маршрутизация', ['steer', 'forkozz', 'mixomo', 'awg']],
      ['Сеть', ['hosts', 'doh', 'tgproxy']], ['', ['system']],
    ];
    let h = '';
    for (const [g, ids] of groups) {
      if (g) h += `<div class="grp">${g}</div>`;
      for (const id of ids) {
        const p = this.pages[id];
        if (!p) continue;
        const st = p.dot ? p.dot(this.dash) : '';
        h += `<a href="#${id}" class="${this.cur === id ? 'on' : ''}">${esc(p.title)}${st ? `<span class="dot ${st}"></span>` : ''}</a>`;
      }
    }
    document.getElementById('nav').innerHTML = h;
  },

  async go(id) {
    if (!this.pages[id]) id = 'dash';
    this.cur = id;
    document.getElementById('side').classList.remove('open');
    document.getElementById('title').textContent = this.pages[id].title;
    this.nav();
    const el = document.getElementById('page');
    el.innerHTML = '<div class="empty"><span class="spin"></span></div>';
    el.dataset.page = id;
    await this.render();
  },

  async render() {
    const id = this.cur, el = document.getElementById('page');
    try {
      const html = await this.pages[id].render(el);
      if (this.cur !== id) return;
      if (typeof html === 'string') el.innerHTML = html;
      if (this.pages[id].mounted) this.pages[id].mounted(el);
    } catch (e) {
      el.innerHTML = `<div class="zm-card"><h3>Ошибка</h3><p>${esc(e.message)}</p></div>`;
    }
  },

  // refresh re-renders the current page keeping scroll position and editor contents untouched.
  async refresh() {
    const p = this.pages[this.cur];
    if (p && p.noAutoRefresh) { if (p.update) await p.update(); return; }
    const y = window.scrollY;
    await this.render();
    window.scrollTo(0, y);
  },

  async pollDash() {
    try {
      this.dash = await this.api('status');
      this.nav();
      const jobs = (this.dash.jobs || []).map(j => `<span class="badge warn"><span class="spin"></span>${esc(j.label)}</span>`).join('');
      document.getElementById('topJobs').innerHTML = jobs;
      const ver = document.getElementById('ver');
      if (this.self && this.self.newer) ver.innerHTML = `<button class="btn sm pri" data-selfupd="1" title="Обновить Zapret Manager">↑ ${esc(this.self.latest)}</button>`;
      else ver.textContent = 'v' + this.dash.sys.version + (this.dash.sys.dev ? ' · dev' : '');
      if (!this.selfChecked) { this.selfChecked = true; this.api('self_check').then(r => { this.self = r; this.nav(); if (r.newer) { this.pollDash(); if (this.cur === 'dash') this.refresh(); } }).catch(() => {}); }
      if (this.theme !== this.dash.theme && !this.themeSet) this.applyTheme(this.dash.theme);
      const p = this.pages[this.cur];
      if (p && p.live) p.live(this.dash);
    } catch (e) { /* shown elsewhere */ }
  },

  applyTheme(t) {
    this.theme = t || 'auto';
    const r = document.documentElement;
    if (this.theme === 'ultrakill') { r.setAttribute('data-skin', 'ultrakill'); r.setAttribute('data-theme', 'dark'); }
    else { r.removeAttribute('data-skin'); if (this.theme === 'auto') r.removeAttribute('data-theme'); else r.setAttribute('data-theme', this.theme); }
    try { localStorage.setItem('zm-theme', this.theme); } catch (e) {}
  },

  start() {
    const r0 = document.documentElement;
    this.theme = r0.getAttribute('data-skin') || r0.getAttribute('data-theme') || 'auto';
    document.getElementById('burger').onclick = () => document.getElementById('side').classList.toggle('open');
    document.getElementById('themeBtn').onclick = () => {
      const next = { auto: 'light', light: 'dark', dark: 'ultrakill', ultrakill: 'auto' }[this.theme] || 'auto';
      this.themeSet = true;
      this.applyTheme(next);
      this.toast('Стиль: ' + { auto: 'Claude, как в системе', light: 'Claude, светлый', dark: 'Claude, тёмный', ultrakill: 'ULTRAKILL' }[next]);
      this.api('ui_theme_set', { theme: next }).catch(() => {});
    };
    document.addEventListener('click', e => {
      const b = e.target.closest('[data-act]');
      if (b && !b.disabled) { e.preventDefault(); const p = this.pages[this.cur]; const fn = p && p.act && p.act[b.dataset.act]; if (fn) fn.call(p, b, e); }
      if (e.target.closest('[data-fixconf]')) { e.preventDefault(); fixConflicts(); return; }
      if (e.target.closest('[data-selfupd]')) { e.preventDefault(); updateSelf(); return; }
      const u = e.target.closest('[data-upd]'); if (u) { e.preventDefault(); updateComponent(u.dataset.upd); return; }
      const s = e.target.closest('.secret'); if (s) s.classList.toggle('open');
      const c = e.target.closest('[data-copy]'); if (c) copy(c.dataset.copy);
    });
    document.addEventListener('change', e => {
      const b = e.target.closest('[data-chg]');
      if (b) { const p = this.pages[this.cur]; const fn = p && p.act && p.act[b.dataset.chg]; if (fn) fn.call(p, b, e); }
    });
    window.addEventListener('hashchange', () => this.go(location.hash.slice(1)));
    this.pollDash().then(() => this.go(location.hash.slice(1) || 'dash'));
    this.timer = setInterval(() => this.pollDash(), 5000);
  },
};

// ---------- helpers ----------
function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]); }
function fmtLog(l) {
  const s = esc(l);
  if (/^==>/.test(l)) return `<span class="h">${s}</span>`;
  if (/^(ОШИБКА|!!)/.test(l)) return `<span class="e">${s}</span>`;
  if (/✓/.test(l)) return `<span class="k">${s}</span>`;
  return s;
}
function badge(kind, text) { return `<span class="badge ${kind}"><span class="dot"></span>${esc(text)}</span>`; }
function stBadge(installed, running, txt) {
  if (!installed) return badge('off', (txt && txt.ni) || 'не установлен');
  return running ? badge('ok', (txt && txt.on) || 'работает') : badge('bad', (txt && txt.off) || 'остановлен');
}
function card(title, body, right, cls) {
  return `<div class="zm-card ${cls || ''}"><h3>${title}${right ? `<span class="r">${right}</span>` : ''}</h3>${body}</div>`;
}
function btn(act, label, cls, data) {
  let d = '';
  for (const k in (data || {})) d += ` data-${k}="${esc(data[k])}"`;
  return `<button class="btn ${cls || ''}" data-act="${act}"${d}>${label}</button>`;
}
function tog(id, title, sub, on, chg, data) {
  let d = '';
  for (const k in (data || {})) d += ` data-${k}="${esc(data[k])}"`;
  return `<div class="tog"><div class="t"><b>${title}</b>${sub ? `<small>${sub}</small>` : ''}</div>
    <label class="switch"><input type="checkbox" id="${id}" ${on ? 'checked' : ''} data-chg="${chg}"${d}><i></i></label></div>`;
}
function row(label, val) { return `<div class="zm-row"><span class="lbl">${label}</span><span>${val}</span></div>`; }
function sel(id, opts, cur, chg) {
  return `<select id="${id}" ${chg ? `data-chg="${chg}"` : ''}>${opts.map(o => {
    const [v, t] = Array.isArray(o) ? o : [o, o];
    return `<option value="${esc(v)}" ${String(v) === String(cur) ? 'selected' : ''}>${esc(t)}</option>`;
  }).join('')}</select>`;
}
function val(id) { const e = document.getElementById(id); return e ? (e.type === 'checkbox' ? e.checked : e.value) : ''; }
function field(label, inner) { return `<label class="field"><span>${label}</span>${inner}</label>`; }
function dur(s) {
  s = +s || 0;
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  return d ? `${d} д ${h} ч` : h ? `${h} ч ${m} мин` : `${m} мин`;
}
function bytes(n) { n = +n || 0; const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']; let i = 0; while (n >= 1024 && i < 4) { n /= 1024; i++; } return n.toFixed(i ? 1 : 0) + ' ' + u[i]; }
function when(ts) { return ts ? new Date(ts * 1000).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—'; }
function copy(t) {
  (navigator.clipboard ? navigator.clipboard.writeText(t) : Promise.reject()).then(() => ZM.toast('Скопировано', 'ok')).catch(() => {
    const a = document.createElement('textarea'); a.value = t; document.body.appendChild(a); a.select();
    try { document.execCommand('copy'); ZM.toast('Скопировано', 'ok'); } catch (e) { ZM.toast('Не удалось скопировать — выделите вручную', 'err'); }
    a.remove();
  });
}
function notice(kind, html) { return `<div class="notice ${kind}">${html}</div>`; }
function procInfo(p) {
  if (!p || !p.managed) return '';
  let s = p.running ? `PID ${p.pid}, работает ${dur(p.uptime)}` : 'не запущен';
  if (p.restarts) s += `, перезапусков: ${p.restarts}`;
  if (p.last_error && !p.running) s += ` — ${esc(p.last_error)}`;
  return `<span class="muted">${s}</span>`;
}
// tabs: keeps the selected tab per page
function tabs(page, list, cur) {
  return `<div class="tabs">${list.map(([id, t]) => `<button data-act="tab" data-tab="${id}" class="${cur === id ? 'on' : ''}">${t}</button>`).join('')}</div>`;
}
async function logViewer(name) {
  const r = await ZM.api('proc_log', { name });
  ZM.modal(`<h3>Журнал: ${esc(name)}</h3><pre class="log">${(r.lines || []).map(fmtLog).join('\n') || 'пусто'}</pre>
    <div class="zm-actions"><button class="btn pri" onclick="ZM.closeModal()">Закрыть</button></div>`);
}

// ---------- foreign zapret / GoodbyeDPI ----------
function conflictsBlock(list) {
  if (!list || !list.length) return '';
  return `<div class="notice bad"><b>Найден другой zapret — он перехватывает тот же трафик через WinDivert, и обход работать не будет.</b>
    <ul style="margin:6px 0 8px 18px;padding:0">${list.map(c => `<li>${esc(c.text)}${c.pids ? ` <span class="muted">(PID ${c.pids.join(', ')})</span>` : ''}</li>`).join('')}</ul>
    <button class="btn pri" data-fixconf="1">Отключить сторонний zapret</button>
    <span class="hint">Службы переводятся в ручной запуск (не удаляются), процессы останавливаются.</span></div>`;
}
async function fixConflicts() {
  if (!await ZM.confirm('Отключить сторонний zapret?', 'Службы другого zapret / GoodbyeDPI будут остановлены и выключены из автозапуска, их процессы завершены. Вернуть можно в «Службах» Windows.', 'Отключить')) return false;
  const r = await ZM.call('conflicts_fix', {}, { reload: true });
  if (r) { ZM.toast((r.done || []).join('; ') || 'Готово', 'ok'); ZM.pollDash(); return true; }
  return false;
}
// beforeZapret: if a foreign zapret is running, offer to disable it before starting ours.
async function beforeZapret() {
  let list = [];
  try { list = (await ZM.api('conflicts')).items || []; } catch (e) {}
  if (!list.length) return true;
  const ok = await ZM.confirm('Обнаружен другой zapret', `${list.map(c => esc(c.text)).join('<br>')}<br><br>Пока он работает, наш Zapret не сможет обходить блокировки. Отключить его?`, 'Отключить и продолжить');
  if (!ok) return true;
  const r = await ZM.call('conflicts_fix', {}, { reload: false });
  return !!r;
}

// ---------- updates ----------
function selfBanner() {
  const s = ZM.self;
  if (!s || !s.newer) return '';
  return `<div class="notice info" style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">
    <span>Доступна новая версия Zapret Manager: <b>${esc(s.latest)}</b> (у вас ${esc(s.current)}).</span>
    <button class="btn pri" data-selfupd="1">Обновить</button><a href="${esc(s.url)}" target="_blank" class="hint">что нового</a></div>`;
}
async function updateSelf() {
  if (!await ZM.confirm('Обновить Zapret Manager?', 'Программа скачает новую версию с GitHub, заменит себя и перезапустит службу. Панель переподключится сама; на несколько секунд обход блокировок прервётся.', 'Обновить')) return;
  await ZM.call('self_update', {}, { title: 'Обновление Zapret Manager', reload: false });
  ZM.toast('Служба перезапускается — подождите…');
  const t0 = Date.now();
  await new Promise(r => setTimeout(r, 4000));
  for (;;) {
    try { const r = await fetch('/api/status', { method: 'POST', headers: { 'X-ZM': '1' }, body: '{}' }); if (r.ok) break; } catch (e) {}
    if (Date.now() - t0 > 90000) { ZM.toast('Служба не вернулась за 90 секунд — откройте панель ярлыком', 'err'); return; }
    await new Promise(r => setTimeout(r, 1500));
  }
  location.reload();
}
const UPD_ACTIONS = {
  zapret: ['zapret_action', { action: 'install' }, 'Обновление Zapret'],
  zapret2: ['z2_action', { action: 'install' }, 'Обновление Zapret2'],
  mihomo: ['rt_action', { action: 'install' }, 'Обновление Mihomo'],
  byedpi: ['bt_action', { action: 'install' }, 'Обновление ByeDPI'],
  'tg-go': ['tg_install', { id: 'go' }, 'Обновление TG WS Proxy'],
  'tg-rs': ['tg_install', { id: 'rs' }, 'Обновление TG WS Proxy'],
};
async function updateComponent(id) {
  if (id === 'self') return updateSelf();
  const a = UPD_ACTIONS[id]; if (!a) return;
  await ZM.call(a[0], a[1], { title: a[2] });
  ZM.versCache = null;
}
