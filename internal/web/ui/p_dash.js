'use strict';

ZM.page('dash', {
  title: 'Дашборд',
  async render() {
    const d = ZM.dash || await ZM.api('status');
    return this.html(d);
  },
  live(d) { if (!document.querySelector('.zm-modal:not([hidden])')) document.getElementById('page').innerHTML = this.html(d); },
  html(d) {
    const s = d.sys, z = d.zapret;
    const mem = s.mem_total ? Math.round(s.mem_used * 100 / s.mem_total) : 0;
    const lvl = p => p > 85 ? 'hi' : p > 60 ? 'mid' : '';
    let warn = '';
    if (s.dev) warn += notice('info', 'Режим разработки: системные действия Windows имитируются.');
    warn += conflictsBlock(z.conflicts);
    if (z.installed && !z.ipv6 && false) warn += '';
    if (!d.internet) warn += notice('warn', 'Нет связи с интернетом (google.com:443 не отвечает).');

    const zr = z.installed
      ? `${row('Состояние', stBadge(true, z.running, { off: z.enabled ? 'не запущен' : 'выключен' }))}
         ${row('Версия', esc(z.version || '—'))}
         ${row('Стратегия', `<b>${esc(z.strategy || '—')}</b>`)}
         ${z.flowseal ? row('Flowseal', esc(z.flowseal)) : ''}
         ${row('Процесс', procInfo(z.proc) || '—')}
         <div class="zm-actions">
           ${z.running ? btn('zstop', 'Остановить') : btn('zstart', 'Запустить', 'pri')}
           ${btn('zrestart', 'Перезапустить')}
           <a class="btn" href="#zapret">Стратегии →</a>
         </div>`
      : `<p>Zapret (winws) ещё не установлен. Он обходит DPI-блокировки YouTube, Discord и сайтов — как на роутере.</p>
         <div class="zm-actions">${btn('zinstall', 'Установить Zapret', 'pri')}${btn('zfull', 'Установить и настроить (v7 + hosts + игры)')}</div>`;

    const t = (href, title, st, sub) => `<a class="tile" href="#${href}"><span class="tt"><span class="dot ${st}"></span>${title}</span><span class="ts">${sub}</span></a>`;
    const tgOn = (d.tg || []).filter(x => x.running);
    const awgOn = (d.awg || []).filter(x => x.running);
    const rt = d.routing;
    const rtSub = !rt.installed ? 'движок не установлен' : rt.mode === 'off' ? 'выключено' : (rt.running ? 'работает: ' : 'остановлено: ') + ({ generated: 'Steer / Forkozz', mixomo: 'Mixomo' }[rt.mode] || rt.mode);
    const tiles = [
      t('zapret2', 'Zapret2', d.zapret2.installed ? (d.zapret2.running ? 'ok' : 'bad') : 'off', d.zapret2.installed ? (d.zapret2.running ? 'работает' : 'остановлен') : 'не установлен'),
      t('doh', 'DNS over HTTPS', d.doh.installed ? (d.doh.running ? 'ok' : 'bad') : 'off', d.doh.installed ? esc(d.doh.current) + (d.doh.mode === 'windows' ? ' · Windows 11' : ' · свой прокси') : 'выключен'),
      t('hosts', 'hosts', d.hosts_blocks ? 'ok' : 'off', d.hosts_blocks ? `включено блоков: ${d.hosts_blocks}` : 'блоки не добавлены'),
      t('tgproxy', 'TG WS Proxy', tgOn.length ? 'ok' : (d.tg || []).some(x => x.config.installed) ? 'bad' : 'off', tgOn.length ? tgOn.map(x => x.title.replace('TG WS Proxy ', '')).join(', ') : 'не запущен'),
      t('awg', 'AmneziaWG', awgOn.length ? 'ok' : (d.awg || []).length ? 'bad' : 'off', awgOn.length ? 'туннели: ' + awgOn.map(x => x.name).join(', ') : (d.awg || []).length ? 'туннели остановлены' : 'туннелей нет'),
      t('bytetube', 'ByeTube', d.bytetube.installed ? (d.bytetube.running ? 'ok' : 'bad') : 'off', d.bytetube.installed ? (d.bytetube.running ? 'YouTube через ByeDPI' : 'остановлен') : 'не установлен'),
      t(rt.mode === 'mixomo' ? 'mixomo' : 'forkozz', 'Маршрутизация', rt.mode === 'off' || !rt.installed ? 'off' : rt.running ? 'ok' : 'bad', rtSub),
      t('system', 'QUIC в брандмауэре', d.quic_blocked ? 'warn' : 'off', d.quic_blocked ? 'UDP 443 заблокирован' : 'не блокируется'),
    ].join('');

    return `${warn}${styleMeter(d)}
    <div class="zm-cards c2">
      ${card('Zapret', zr, stBadge(z.installed, z.running))}
      ${card('Компьютер', `
        ${row('Система', esc(s.os) + ' <span class="muted">' + esc(s.build) + '</span>')}
        ${row('Имя', esc(s.host))}
        ${row('Работает', dur(s.uptime))}
        ${row('IP', esc((s.ips || []).join(', ') || '—'))}
        ${row('Интернет', d.internet ? badge('ok', 'есть') : badge('bad', 'нет'))}
        <div class="zm-row" style="display:block"><span class="lbl">Процессор ${s.cpu}%</span><div class="bar ${lvl(s.cpu)}"><i style="width:${s.cpu}%"></i></div></div>
        <div class="zm-row" style="display:block"><span class="lbl">Память ${bytes(s.mem_used)} из ${bytes(s.mem_total)}</span><div class="bar ${lvl(mem)}"><i style="width:${mem}%"></i></div></div>`)}
    </div>
    ${card('Компоненты', `<div class="tiles">${tiles}</div>`)}
    ${card('Обновления', `<div id="vers">${ZM.versCache ? versTable(ZM.versCache) : '<p class="hint">Сравнить установленные версии с последними релизами на GitHub.</p>'}</div>`, btn('vers', 'Проверить', 'sm'))}`;
  },
  act: {
    async zstart() { if (await beforeZapret()) ZM.call('zapret_action', { action: 'start' }, { ok: 'Zapret запущен' }); },
    zstop() { ZM.call('zapret_action', { action: 'stop' }, { ok: 'Zapret остановлен' }); },
    zrestart() { ZM.call('zapret_action', { action: 'restart' }, { ok: 'Zapret перезапущен' }); },
    async zinstall() { if (await beforeZapret()) ZM.call('zapret_action', { action: 'install' }, { title: 'Установка Zapret' }); },
    async zfull() {
      if (!await beforeZapret()) return;
      if (await ZM.confirm('Установить и настроить Zapret?', 'Поставим Zapret, применим стратегию v7, добавим популярные блоки в hosts и игровую стратегию Gv1 — как пункт «f» в меню на роутере.'))
        ZM.call('zapret_action', { action: 'full' }, { title: 'Установка и настройка Zapret' });
    },
    async vers(b) {
      b.disabled = true; b.innerHTML = '<span class="spin"></span>';
      try {
        const r = await ZM.api('versions', { refresh: true });
        ZM.versCache = r.items;
        document.getElementById('vers').innerHTML = versTable(r.items);
      } catch (e) { ZM.toast(e.message, 'err'); }
      b.disabled = false; b.textContent = 'Проверить';
    },
  },
});

function versTable(items) {
  return `<div class="scroll"><table class="zm"><tr><th>Компонент</th><th>Установлен</th><th>Последний</th><th></th></tr>${items.map(i =>
    `<tr><td>${esc(i.name)}</td><td>${esc(i.installed || '—')}</td><td>${esc(i.latest || '—')}</td><td>${i.newer ? badge('warn', 'есть обновление') : i.installed ? badge('ok', 'актуально') : ''}</td></tr>`).join('')}</table></div>`;
}

// STYLE meter for the ULTRAKILL skin: the more of the arsenal is running, the higher the rank.
const UK_RANKS = [['D', 'Destructive'], ['C', 'Chaotic'], ['B', 'Brutal'], ['A', 'Anarchic'], ['S', 'Supreme'], ['SS', 'SSadistic'], ['SSS', 'SSShitstorm'], ['ULTRAKILL', '']];
function styleMeter(d) {
  const feed = [];
  if (d.zapret.running) feed.push('Zapret online');
  if (d.zapret.running && /v\d|general|Custom/.test(d.zapret.strategy || '')) feed.push('Strategy ' + (d.zapret.strategy || '').split(' ')[0]);
  if (d.zapret.quic_yt) feed.push('QUIC breaker');
  if (d.zapret2.running) feed.push('Zapret2');
  if (d.doh.installed && d.doh.running) feed.push('DNS encrypted');
  if (d.hosts_blocks) feed.push('Hosts x' + d.hosts_blocks);
  if ((d.tg || []).some(t => t.running)) feed.push('Telegram freed');
  if ((d.awg || []).some(t => t.running)) feed.push('Tunnel up');
  if (d.bytetube.running) feed.push('ByeTube');
  if (d.routing.running) feed.push('Routing ' + ({ generated: 'steer', mixomo: 'mixomo' }[d.routing.mode] || ''));
  if (d.internet) feed.push('Uplink');
  const score = feed.length;
  const idx = Math.min(UK_RANKS.length - 1, Math.max(0, score - 1));
  const [rk, name] = UK_RANKS[idx];
  const pct = Math.min(100, Math.round(score * 100 / 9));
  return `<div class="zm-card uk-only uk-style"><h3>Style</h3>
    <div class="uk-meter"><div class="uk-rank uk-${rk}">${rk}</div>
      <div style="flex:1;min-width:0"><div class="uk-rank-name uk-${rk}">${name ? `<span style="font-size:1.25em">${rk.charAt(0)}</span>${esc(name.slice(1))}` : 'Ultrakill'}</div>
      <div class="uk-meter-bar uk-${rk}"><i style="width:${pct}%"></i></div>
      <div class="uk-feed">${feed.map(f => `<span>${esc(f)}</span>`).join('') || '<span>No style. Get moving.</span>'}</div></div></div></div>`;
}
