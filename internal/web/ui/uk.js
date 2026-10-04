'use strict';
// ULTRAKILL skin engine: blood, screen shake, style meter, damage flash, HUD and user art slots.
// Everything is drawn procedurally; game art comes only from the user's own Steam cache / uploads.

const UK = {
  on: false, points: 0, rankIdx: -1, art: {}, last: 0, combo: 0,
  RANKS: [['D', 'Destructive', '#2f9bff'], ['C', 'Chaotic', '#4dff7c'], ['B', 'Brutal', '#ffd21e'], ['A', 'Anarchic', '#ff8a1e'],
    ['S', 'Supreme', '#ff1e1e'], ['SS', 'SSadistic', '#ff1e1e'], ['SSS', 'SSShitstorm', '#ff1e1e'], ['ULTRAKILL', '', '#ffd700']],
  BONUS: ['+PARRY', '+RICOSHOT', '+DISRESPECT', '+LIMB HIT', '+FRIED', '+INSTAKILL', '+CHARGEBACK', '+SPLATTERED', '+BIG KILL', '+STYLE'],
  STEP: 120,

  init() {
    const mk = (id, z) => { const c = document.createElement('canvas'); c.id = id; c.style.cssText = `position:fixed;inset:0;pointer-events:none;z-index:${z}`; document.body.appendChild(c); return c; };
    this.stains = mk('ukStains', 0);
    this.blood = mk('ukBlood', 65);
    document.body.insertAdjacentHTML('beforeend', '<div id="ukFlash"></div>');
    // style meter lives in the page header, HP in the sidebar — never over the content
    document.querySelector('.zm-top').insertAdjacentHTML('beforeend', `
      <div id="ukHud" class="uk-only">
        <div class="uk-hud-rank"><span id="ukRank">D</span><b id="ukRankName">Destructive</b></div>
        <div class="uk-hud-bar"><i id="ukBar"></i></div>
        <div id="ukPops"></div>
      </div>`);
    document.querySelector('.zm-side-foot').insertAdjacentHTML('beforebegin', `
      <div id="ukHp" class="uk-only"><div class="uk-hp-row"><span>HP</span><div class="uk-hp-bar"><i id="ukHpBar"></i></div><b id="ukHpNum">100</b></div>
        <div class="uk-stam"><i></i><i></i><i></i></div></div>`);
    this.resize();
    window.addEventListener('resize', () => this.resize());
    document.addEventListener('pointerdown', e => {
      if (!this.on) return;
      const b = e.target.closest('.btn, .tile, #nav a, .tabs button, .switch');
      if (b) this.hit(e.clientX, e.clientY, b.classList.contains('dng') ? 2 : 1);
    }, true);
    const toast = ZM.toast.bind(ZM);
    ZM.toast = (msg, kind) => {
      toast(msg, kind);
      if (!this.on) return;
      if (kind === 'err') this.damage();
      else if (kind === 'ok') this.style(60, this.BONUS[Math.floor(Math.random() * this.BONUS.length)]);
    };
    const apply = ZM.applyTheme.bind(ZM);
    ZM.applyTheme = t => { apply(t); this.toggle(t === 'ultrakill'); };
    this.toggle(document.documentElement.getAttribute('data-skin') === 'ultrakill');
    setInterval(() => this.tick(), 100);
    if (/[?&]demo\b/.test(location.search)) setTimeout(() => this.demo(), 1200);
  },
  // demo: a burst of action for screenshots (?demo)
  demo() {
    const pts = [[0.42, 0.3], [0.7, 0.55], [0.3, 0.72], [0.82, 0.25], [0.55, 0.85], [0.18, 0.4]];
    pts.forEach(([x, y], i) => setTimeout(() => this.hit(x * innerWidth, y * innerHeight, 1 + (i % 2)), i * 90));
    ['+PARRY', '+RICOSHOT', '+DISRESPECT', '+INSTAKILL'].forEach((b, i) => setTimeout(() => this.style(130, b), 200 + i * 120));
  },

  toggle(on) {
    this.on = on;
    if (on) { this.loadArt(); this.drawGore(); } else { this.clear(this.stains); this.clear(this.blood); }
  },

  resize() {
    for (const c of [this.stains, this.blood]) {
      const keep = c === this.stains && c.width ? c.toDataURL() : null;
      c.width = innerWidth; c.height = innerHeight;
      if (keep) { const i = new Image(); i.onload = () => c.getContext('2d').drawImage(i, 0, 0); i.src = keep; }
    }
  },
  clear(c) { c.getContext('2d').clearRect(0, 0, c.width, c.height); },

  async loadArt() {
    try {
      const r = await ZM.api('skin_list', { skin: 'ultrakill' });
      this.art = r.have || {};
    } catch (e) { this.art = {}; }
    const root = document.documentElement.style, v = Date.now();
    root.setProperty('--uk-bg', this.art.bg ? `url("/skin/ultrakill/bg?v=${v}")` : 'none');
    root.setProperty('--uk-portrait', this.art.portrait ? `url("/skin/ultrakill/portrait?v=${v}")` : 'none');
    root.setProperty('--uk-logo', this.art.logo ? `url("/skin/ultrakill/logo?v=${v}")` : 'none');
    document.documentElement.classList.toggle('uk-has-bg', !!this.art.bg);
    document.documentElement.classList.toggle('uk-has-portrait', !!this.art.portrait);
    document.documentElement.classList.toggle('uk-has-logo', !!this.art.logo);
  },

  // ---------- blood ----------
  splat(ctx, x, y, size, alpha) {
    const reds = ['#5a0000', '#7a0000', '#8f0505', '#a30000', '#600000', '#3a0000'];
    ctx.save();
    ctx.globalAlpha = alpha;
    ctx.fillStyle = reds[Math.floor(Math.random() * reds.length)];
    ctx.beginPath();
    const pts = 14 + Math.floor(Math.random() * 10);
    for (let i = 0; i <= pts; i++) {
      const a = i / pts * Math.PI * 2, r = size * (0.55 + Math.random() * 0.6);
      const px = x + Math.cos(a) * r, py = y + Math.sin(a) * r;
      i ? ctx.lineTo(px, py) : ctx.moveTo(px, py);
    }
    ctx.closePath(); ctx.fill();
    const drops = 10 + Math.floor(size / 2);
    for (let i = 0; i < drops; i++) {
      const a = Math.random() * Math.PI * 2, d = size * (1 + Math.random() * 3.2), r = Math.max(1, size * (0.05 + Math.random() * 0.22));
      ctx.beginPath(); ctx.ellipse(x + Math.cos(a) * d, y + Math.sin(a) * d, r, r * (0.6 + Math.random()), a, 0, Math.PI * 2); ctx.fill();
      if (Math.random() < 0.35) { // streak
        ctx.lineWidth = Math.max(1, r * 0.8); ctx.strokeStyle = ctx.fillStyle;
        ctx.beginPath(); ctx.moveTo(x + Math.cos(a) * size * 0.8, y + Math.sin(a) * size * 0.8); ctx.lineTo(x + Math.cos(a) * d, y + Math.sin(a) * d); ctx.stroke();
      }
    }
    ctx.restore();
  },
  drip(ctx, x, y, len) {
    ctx.save(); ctx.fillStyle = '#6e0000'; ctx.globalAlpha = 0.85;
    const w = 2 + Math.random() * 4;
    ctx.fillRect(x - w / 2, y, w, len);
    ctx.beginPath(); ctx.arc(x, y + len, w * 0.9, 0, Math.PI * 2); ctx.fill();
    ctx.restore();
  },
  hit(x, y, power) {
    const b = this.blood.getContext('2d'), s = this.stains.getContext('2d');
    this.splat(b, x, y, 16 * power + Math.random() * 14, 0.95);
    this.splat(s, x + (Math.random() - .5) * 30, y + (Math.random() - .5) * 30, 12 * power + Math.random() * 12, 0.4);
    if (Math.random() < 0.5) this.drip(s, x + (Math.random() - .5) * 40, y, 20 + Math.random() * 80);
    this.bloodAlpha = 1; this.bloodAt = Date.now();
    this.shake(power * 4);
    this.style(18 * power);
  },
  drawGore() {
    // ambient gore on the page edges so the screen never looks clean
    const s = this.stains.getContext('2d');
    for (let i = 0; i < 9; i++) {
      const edge = Math.random() < .5;
      const x = edge ? (Math.random() < .5 ? Math.random() * 120 : innerWidth - Math.random() * 120) : Math.random() * innerWidth;
      const y = edge ? Math.random() * innerHeight : (Math.random() < .5 ? Math.random() * 90 : innerHeight - Math.random() * 90);
      this.splat(s, x, y, 18 + Math.random() * 40, 0.3);
    }
    for (let i = 0; i < 14; i++) this.drip(s, Math.random() * innerWidth, 0, 30 + Math.random() * 160);
  },
  shake(px) {
    const el = document.querySelector('.zm-main');
    if (!el) return;
    el.animate([{ transform: `translate(${px}px,${-px / 2}px)` }, { transform: `translate(${-px}px,${px / 2}px)` }, { transform: `translate(${px / 2}px,0)` }, { transform: 'none' }], { duration: 180 });
  },
  damage() {
    const f = document.getElementById('ukFlash');
    f.classList.remove('hit'); void f.offsetWidth; f.classList.add('hit');
    this.shake(14);
    this.points = Math.max(0, this.points - 240);
    this.pop('-HURT', '#ff1e1e');
    for (let i = 0; i < 4; i++) this.splat(this.blood.getContext('2d'), Math.random() * innerWidth, Math.random() * innerHeight, 30 + Math.random() * 40, 0.8);
    this.bloodAlpha = 1; this.bloodAt = Date.now();
  },

  // ---------- style ----------
  base() {
    const d = ZM.dash; if (!d) return 0;
    let n = 0;
    if (d.zapret.running) n += 2;
    if (d.doh.installed && d.doh.running) n++;
    if (d.hosts_blocks) n++;
    if ((d.tg || []).some(t => t.running)) n++;
    if ((d.awg || []).some(t => t.running)) n++;
    if (d.bytetube.running) n++;
    if (d.routing.running) n++;
    if (d.zapret2.running) n++;
    return n * 25;
  },
  style(pts, label) {
    this.points = Math.min(this.STEP * 8 - 1, this.points + pts);
    this.last = Date.now();
    if (label) this.pop(label);
  },
  pop(text, color) {
    const box = document.getElementById('ukPops');
    if (!box) return;
    const p = document.createElement('div');
    p.className = 'uk-pop'; p.textContent = text; if (color) p.style.color = color;
    box.prepend(p);
    while (box.children.length > 6) box.lastChild.remove();
    setTimeout(() => p.remove(), 2600);
  },
  tick() {
    if (!this.on) return;
    // fade fresh blood off the top layer
    // fresh blood hangs for ~2.5 s, then slowly drains away (stains stay on the background)
    if (this.bloodAlpha > 0 && Date.now() - this.bloodAt > 2500) {
      const c = this.blood, x = c.getContext('2d');
      x.save(); x.globalCompositeOperation = 'destination-out'; x.fillStyle = 'rgba(0,0,0,0.035)'; x.fillRect(0, 0, c.width, c.height); x.restore();
      this.bloodAlpha -= 0.02;
    }
    // style decays like in the game, never below what the running arsenal earns
    const floor = this.base();
    if (Date.now() - this.last > 1500) this.points = Math.max(floor, this.points - 3);
    if (this.points < floor) this.points = floor;
    const idx = Math.min(this.RANKS.length - 1, Math.floor(this.points / this.STEP));
    const [rk, name, col] = this.RANKS[idx];
    const pct = (this.points % this.STEP) / this.STEP * 100;
    const r = document.getElementById('ukRank'), n = document.getElementById('ukRankName'), bar = document.getElementById('ukBar');
    if (!r) return;
    if (idx !== this.rankIdx) {
      if (idx > this.rankIdx && this.rankIdx >= 0) { this.pop('RANK UP', col); this.shake(6); }
      this.rankIdx = idx;
      const slot = 'rank_' + rk.toLowerCase();
      if (this.art[slot]) r.innerHTML = `<img src="/skin/ultrakill/${slot}" alt="${rk}" style="height:56px;vertical-align:bottom">`; else r.textContent = rk;
      n.textContent = name || 'ULTRAKILL';
      r.style.color = n.style.color = bar.style.background = col;
      r.classList.remove('bump'); void r.offsetWidth; r.classList.add('bump');
    }
    bar.style.width = (idx === this.RANKS.length - 1 ? 100 : pct) + '%';
    document.getElementById('ukHud').classList.toggle('idle', Date.now() - this.last > 6000);
    // HP: internet + zapret + no conflicts
    const d = ZM.dash;
    if (d) {
      let hp = 100;
      if (!d.internet) hp -= 45;
      if (d.zapret.installed && !d.zapret.running) hp -= 30;
      hp -= 15 * ((d.zapret.conflicts || []).length);
      hp = Math.max(5, hp);
      document.getElementById('ukHpBar').style.width = hp + '%';
      document.getElementById('ukHpNum').textContent = hp;
    }
  },
};

UK.init();
