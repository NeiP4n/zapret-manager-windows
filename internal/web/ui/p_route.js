'use strict';
// Steer / Forkozz / Mixomo — one mihomo engine in TUN mode behind three pages.

async function rtStatus() { return ZM.api('rt_status'); }

function engineCard(s, page) {
  if (!s.installed) return card('Движок Mihomo не установлен', `
    <p>Steer, Forkozz и Mixomo направляют выбранные сайты в VPN через <b>mihomo</b> в режиме TUN — так же, как sing-box/mihomo на роутере, только для этого компьютера.</p>
    <div class="zm-actions">${btn('rtInstall', 'Установить Mihomo', 'pri')}</div>`, badge('off', 'не установлен'));
  const modeT = { off: 'выключено', generated: 'Steer + Forkozz', mixomo: 'Mixomo (своя конфигурация)' }[s.mode];
  let warn = '';
  if (page !== 'mixomo' && s.mode === 'mixomo') warn = notice('warn', 'Сейчас работает Mixomo со своей конфигурацией — настройки этой страницы применятся, когда Mixomo выключен.');
  if (page === 'mixomo' && s.mode === 'generated') warn = notice('info', 'Сейчас работают Steer / Forkozz. Включение Mixomo заменит их своей конфигурацией.');
  return warn + card(`Движок Mihomo <span class="muted" style="font:400 12px var(--font)">${esc(s.config.version)}</span>`, `
    ${row('Режим', esc(modeT))}${row('Процесс', procInfo(s.proc) || '—')}${row('Списки обновлены', when(s.lists_at))}
    ${!s.running && s.mode !== 'off' && s.log && s.log.length ? `<pre class="log" style="max-height:120px">${s.log.slice(-4).map(fmtLog).join('\n')}</pre>` : ''}
    <div class="zm-actions">${s.mode !== 'off' ? (s.running ? btn('rtStop', 'Остановить') : btn('rtStart', 'Запустить', 'pri')) + btn('rtRestart', 'Перезапустить') : ''}
      ${btn('rtLists', 'Обновить списки')}${btn('rtLog', 'Журнал')}
      ${s.ui ? `<a class="btn" href="${esc(s.ui_url)}" target="_blank">Веб-интерфейс ↗</a>` : btn('rtUI', 'Поставить веб-интерфейс')}
      <span class="right"></span>${btn('rtRemove', 'Удалить', 'dng')}</div>`,
    s.mode === 'off' ? badge('off', 'выключено') : stBadge(true, s.running));
}

const rtActs = {
  rtInstall() { ZM.call('rt_action', { action: 'install' }, { title: 'Установка Mihomo' }); },
  rtUI() { ZM.call('rt_action', { action: 'ui' }, { title: 'Веб-интерфейс metacubexd' }); },
  async rtRemove() { if (await ZM.confirm('Удалить Mihomo?', 'Будут удалены движок, списки, ключи WARP Steer и все настройки Steer / Forkozz / Mixomo.', 'Удалить', true)) ZM.call('rt_action', { action: 'remove' }, { title: 'Удаление Mihomo' }); },
  rtStart() { ZM.call('rt_action', { action: 'start' }, { ok: 'Запущено' }); },
  rtStop() { ZM.call('rt_action', { action: 'stop' }, { ok: 'Остановлено' }); },
  rtRestart() { ZM.call('rt_action', { action: 'restart' }, { ok: 'Перезапущено' }); },
  rtLists() { ZM.call('rt_action', { action: 'lists' }, { title: 'Обновление списков' }); },
  rtLog() { logViewer('mihomo'); },
  async explain() {
    const t = val('exT'); if (!t) return;
    const r = await ZM.api('rt_explain', { target: t }).catch(e => ({ route: 'ошибка', reason: e.message }));
    document.getElementById('exR').innerHTML = `<b>${esc(r.target || t)}</b> → ${badge(r.route === 'напрямую' ? 'off' : 'ok', r.route)} <span class="muted">${esc(r.reason)}</span>`;
  },
  async groups() {
    const box = document.getElementById('groups');
    box.innerHTML = '<span class="spin"></span>';
    try {
      const r = await ZM.api('rt_groups');
      box.innerHTML = (r.groups || []).map(g => `<div style="margin:10px 0"><b>${esc(g.name)}</b> <span class="muted">${esc(g.type)} → ${esc(g.now || '—')}</span> ${btn('delay', 'Проверить задержку', 'sm', { g: g.name })}
        <div class="grid-btns" style="margin-top:6px">${(g.nodes || []).map(n => `<button class="btn sm ${g.now === n.name ? 'on' : ''}" data-act="pick" data-g="${esc(g.name)}" data-n="${esc(n.name)}" ${g.type !== 'Selector' ? 'disabled' : ''}>${esc(n.name)}${n.delay ? ` <span class="muted">${n.delay} мс</span>` : ''}</button>`).join('')}</div></div>`).join('') || '<p class="empty">Групп нет</p>';
    } catch (e) { box.innerHTML = `<p class="hint">${esc(e.message)}</p>`; }
  },
  async delay(b) { b.innerHTML = '<span class="spin"></span>'; await ZM.api('rt_delay', { group: b.dataset.g }).catch(e => ZM.toast(e.message, 'err')); rtActs.groups(); },
  async pick(b) { try { await ZM.api('rt_select', { group: b.dataset.g, node: b.dataset.n }); ZM.toast('Выбран ' + b.dataset.n, 'ok'); rtActs.groups(); } catch (e) { ZM.toast(e.message, 'err'); } },
};

function explainCard() {
  return card('Куда пойдёт сайт', `<p class="hint">Введите сайт или IP — панель покажет, пойдёт он через VPN/WARP или напрямую.</p>
    <div class="zm-row"><input type="text" id="exT" placeholder="youtube.com" style="flex:1;min-width:200px">${btn('explain', 'Проверить')}</div><div id="exR" class="zm-row"></div>`);
}

function serversCard() {
  return card('Серверы и группы', `<div id="groups"><p class="hint">Список групп mihomo с задержками. В группах «выбор» можно закрепить сервер.</p></div>`, btn('groups', 'Показать', 'sm'));
}

function svcChecks(services, chosen, prefix) {
  const set = new Set(chosen || []);
  return `<div class="checks">${services.map(s => `<label class="chk"><input type="checkbox" class="${prefix}" value="${s.id}" ${set.has(s.id) ? 'checked' : ''}> ${esc(s.title)}</label>`).join('')}</div>`;
}
function checked(cls) { return Array.from(document.querySelectorAll('input.' + cls + ':checked')).map(x => x.value); }

// ---------------- Steer ----------------
ZM.page('steer', {
  title: 'Steer',
  async render() {
    const s = await rtStatus(), st = s.config.steer;
    let h = engineCard(s, 'steer');
    if (!s.installed) return h;
    return h + `<div class="zm-cards c2">
      ${card('Steer: сервисы через WARP', `
        <p class="hint">Бесплатные туннели Cloudflare WARP с маскировкой AmneziaWG. Панель получает ключи, поднимает несколько туннелей и сама выбирает самый быстрый живой. Выбранные сервисы идут через WARP, остальное — напрямую.</p>
        ${tog('stOn', 'Steer включён', `Туннелей WARP: ${s.warp_accounts}`, st.enabled, 'noop')}
        <div class="row2">${field('Количество туннелей', sel('stN', [[1, '1'], [2, '2'], [3, '3']], st.tunnels))}
        ${field('Маскировка I1', sel('stI1', [['quic', 'QUIC'], ['quic2', 'QUIC 2'], ['dns', 'DNS'], ['stun', 'STUN'], ['icloud', 'iCloud'], ['sip', 'SIP'], ['none', 'без I1']], st.i1))}</div>
        ${field('Точка входа (пусто — разные для туннелей)', `<input type="text" id="stEp" value="${esc(st.endpoint)}" placeholder="162.159.192.1:2408">`)}
        <div class="zm-actions">${btn('save', 'Сохранить и применить', 'pri')}${btn('recreate', 'Новые ключи WARP')}</div>`)}
      ${card('Что пускать через WARP', `${svcChecks(s.services, st.services, 'stsvc')}
        ${field('Свои домены', `<textarea class="mono" id="stDom" style="min-height:90px" placeholder="chatgpt.com&#10;claude.ai">${esc(st.domains)}</textarea>`)}`)}
    </div>${explainCard()}${serversCard()}`;
  },
  act: Object.assign({}, rtActs, {
    noop() {},
    save() { ZM.call('rt_steer', { steer: { enabled: val('stOn'), tunnels: +val('stN'), i1: val('stI1'), endpoint: val('stEp'), services: checked('stsvc'), domains: val('stDom') } }, { title: 'Steer' }); },
    async recreate() { if (await ZM.confirm('Получить новые ключи WARP?', 'Туннели переподключатся. Помогает, если Cloudflare перестал пускать старые ключи.')) ZM.call('rt_warp_recreate', {}, { title: 'Новые ключи WARP' }); },
  }),
});

// ---------------- Forkozz ----------------
ZM.page('forkozz', {
  title: 'Forkozz',
  dot: d => d && d.routing.mode === 'generated' ? (d.routing.running ? 'ok' : 'bad') : '',
  async render() {
    const s = this.s = await rtStatus(), c = s.config;
    let h = engineCard(s, 'forkozz');
    if (!s.installed) return h;
    const modeT = { links: 'ссылки', subscription: 'подписка', warp: 'WARP', interface: 'интерфейс' };
    const secs = (c.sections || []).map((x, i) => `<div class="tile" style="cursor:default">
      <span class="tt"><span class="dot ${x.enabled ? 'ok' : 'off'}"></span>${esc(x.name)} <span class="muted" style="font-weight:400">· ${modeT[x.mode]}${x.pinned ? ' · ' + esc(x.pinned) : ''}</span>
        <span class="right">${btn('move', '↑', 'sm', { id: x.id, d: -1 })}${btn('move', '↓', 'sm', { id: x.id, d: 1 })}${btn('edit', 'Изменить', 'sm', { i: i })}${btn('del', '✕', 'sm dng', { id: x.id })}</span></span>
      <span class="ts">${(x.services || []).map(id => (s.services.find(v => v.id === id) || {}).title || id).join(', ') || 'без готовых списков'}${x.domains ? ' · свои домены' : ''}${x.subnets ? ' · подсети' : ''}</span></div>`).join('');
    return h + `
      ${card('Секции', `<p class="hint">Каждая секция — способ подключения (VPN-ссылки, подписка, WARP или сетевой интерфейс, например туннель AmneziaWG) и что через него пускать. Правила проверяются сверху вниз.</p>
        <div style="display:flex;flex-direction:column;gap:8px">${secs || '<p class="empty">Секций нет</p>'}</div>`, btn('edit', '+ Секция', 'sm pri', { i: -1 }))}
      <div class="zm-cards c2">
      ${card('Общие настройки', `
        ${field('Исключения — всегда напрямую (домены, IP, подсети)', `<textarea class="mono" id="gEx" style="min-height:90px" placeholder="sberbank.ru&#10;203.0.113.0/24">${esc(c.exclude)}</textarea>`)}
        <div class="row2">${field('DNS', sel('gDns', [['fake-ip', 'fake-ip (быстрее)'], ['redir-host', 'redir-host (реальные IP)']], c.dns_mode))}
        ${field('Обновлять списки', sel('gIv', [['1h', 'каждый час'], ['3h', 'каждые 3 ч'], ['12h', 'каждые 12 ч'], ['1d', 'раз в день'], ['3d', 'раз в 3 дня'], ['off', 'не обновлять']], c.lists_interval))}</div>
        <label class="chk"><input type="checkbox" id="gQ" ${c.block_quic ? 'checked' : ''}> Блокировать QUIC для сайтов из секций (быстрый переход на TCP)</label>
        <div class="zm-actions">${btn('globals', 'Сохранить', 'pri')}</div>`)}
      ${explainCard()}
      </div>${serversCard()}`;
  },
  act: Object.assign({}, rtActs, {
    async move(b) { await ZM.call('rt_section_move', { id: b.dataset.id, dir: +b.dataset.d }, { ok: 'Порядок изменён' }); },
    async del(b) { if (await ZM.confirm('Удалить секцию?', '', 'Удалить', true)) ZM.call('rt_section_delete', { id: b.dataset.id }, { ok: 'Секция удалена' }); },
    globals() { ZM.call('rt_globals', { globals: { exclude: val('gEx'), dns_mode: val('gDns'), lists_interval: val('gIv'), block_quic: val('gQ') } }, { ok: 'Сохранено' }); },
    async edit(b) {
      const i = +b.dataset.i, s = this.s;
      const x = i >= 0 ? s.config.sections[i] : { id: '', name: '', enabled: true, mode: 'links', services: [], sub_interval: '6h' };
      const aw = await ZM.api('awg_status').catch(() => ({ tunnels: [] }));
      const d = ZM.modal(`<h3>${i >= 0 ? 'Секция «' + esc(x.name) + '»' : 'Новая секция'}</h3>
        <div class="row2">${field('Название', `<input type="text" id="sName" value="${esc(x.name)}" placeholder="VPN">`)}
        ${field('Подключение', sel('sMode', [['links', 'VPN-ссылки (vless, vmess, trojan, ss, hy2…)'], ['subscription', 'Подписка'], ['warp', 'WARP (туннели Steer)'], ['interface', 'Сетевой интерфейс']], x.mode, 'smode'))}</div>
        <div id="mLinks">${field('Ссылки — по одной в строке', `<textarea class="mono" id="sLinks" style="min-height:110px">${esc(x.links || '')}</textarea>`)}</div>
        <div id="mSub">${field('Ссылки на подписку (до 10)', `<textarea class="mono" id="sSub" style="min-height:70px">${esc(x.sub_urls || '')}</textarea>`)}
          <div class="row2">${field('Обновлять', sel('sIv', [['30m', '30 мин'], ['1h', '1 ч'], ['3h', '3 ч'], ['6h', '6 ч'], ['12h', '12 ч'], ['1d', '1 день']], x.sub_interval || '6h'))}
          ${field('Только серверы со словами', `<input type="text" id="sF" value="${esc(x.filter || '')}" placeholder="NL, DE">`)}</div>
          ${field('Скрыть серверы со словами', `<input type="text" id="sXF" value="${esc(x.exclude_filter || '')}" placeholder="RU, Russia">`)}</div>
        <div id="mIf">${field('Интерфейс Windows', `<input type="text" id="sIf" list="ifl" value="${esc(x.iface || '')}" placeholder="например, имя туннеля AmneziaWG"><datalist id="ifl">${(aw.tunnels || []).map(t => `<option value="${esc(t.name)}">`).join('')}</datalist>`)}</div>
        <div id="mPin">${field('Сервер', `<input type="text" id="sPin" value="${esc(x.pinned || '')}" placeholder="пусто — автовыбор самого быстрого">`)}</div>
        <b>Что пускать через секцию</b>${svcChecks(s.services, x.services, 'ssvc')}
        <div class="row2">${field('Свои домены', `<textarea class="mono" id="sDom" style="min-height:80px">${esc(x.domains || '')}</textarea>`)}
        ${field('Свои IP и подсети', `<textarea class="mono" id="sNet" style="min-height:80px">${esc(x.subnets || '')}</textarea>`)}</div>
        ${field('Ссылки на списки доменов или подсетей (.lst / .txt)', `<textarea class="mono" id="sLU" style="min-height:50px">${esc(x.list_urls || '')}</textarea>`)}
        <label class="chk"><input type="checkbox" id="sOn" ${x.enabled ? 'checked' : ''}> Секция включена</label>
        <div class="zm-actions"><button class="btn" data-x="no">Отмена</button><button class="btn pri" data-x="yes">Сохранить и применить</button></div>`);
      const showMode = () => {
        const m = d.querySelector('#sMode').value;
        d.querySelector('#mLinks').hidden = m !== 'links';
        d.querySelector('#mSub').hidden = m !== 'subscription';
        d.querySelector('#mIf').hidden = m !== 'interface';
        d.querySelector('#mPin').hidden = m !== 'links' && m !== 'subscription';
      };
      showMode();
      d.querySelector('#sMode').addEventListener('change', showMode);
      d.addEventListener('click', async e => {
        const a = e.target.dataset.x; if (!a) return;
        if (a === 'no') return ZM.closeModal();
        const sec = { id: x.id, name: val('sName'), enabled: val('sOn'), mode: val('sMode'), links: val('sLinks'), sub_urls: val('sSub'), sub_interval: val('sIv'),
          filter: val('sF'), exclude_filter: val('sXF'), iface: val('sIf'), pinned: val('sPin'), services: checked('ssvc'), domains: val('sDom'), subnets: val('sNet'), list_urls: val('sLU') };
        ZM.closeModal();
        await ZM.call('rt_section_save', { section: sec }, { title: 'Применение секции' });
      });
    },
  }),
});

// ---------------- Mixomo ----------------
ZM.page('mixomo', {
  title: 'Mixomo',
  dot: d => d && d.routing.mode === 'mixomo' ? (d.routing.running ? 'ok' : 'bad') : '',
  async render() {
    const s = await rtStatus(), m = s.config.mixomo;
    let h = engineCard(s, 'mixomo');
    if (!s.installed) return h;
    const conf = await ZM.api('rt_mixomo_get');
    const presets = s.services.slice(0, 9);
    const chosen = (m.list_preset || 'russia_inside').split(',');
    return h + `<div class="zm-cards c2">
      ${card('Mixomo по подписке', `<p class="hint">Mihomo со своей VPN-подпиской: через VPN идут сайты из выбранных списков, остальное — напрямую. Конфигурация создаётся автоматически, её можно поправить ниже.</p>
        ${field('Ссылка на подписку', `<input type="text" id="mSub" style="width:100%" value="${esc(m.sub_url)}" placeholder="https://…">`)}
        ${field('Скрыть серверы со словами', `<input type="text" id="mF" value="${esc(m.filter)}" placeholder="RU, Russia, Россия">`)}
        <b>Через VPN</b>${svcChecks(presets, chosen, 'mpre')}
        <label class="chk" style="margin-top:8px"><input type="checkbox" id="mW" ${m.warp_proxy ? 'checked' : ''}> Добавить WARP как запасной сервер</label>
        <div class="zm-actions">${btn('gen', 'Создать конфигурацию и запустить', 'pri')}</div>`)}
      ${card('Работа', `
        ${tog('mOn', 'Mixomo включён', 'Использовать свою конфигурацию вместо Steer / Forkozz', m.enabled, 'onoff')}
        ${field('Ежедневный перезапуск (ЧЧ:ММ, пусто — нет)', `<input type="time" id="mAR" value="${esc(m.auto_restart)}">`)}
        <div class="zm-actions">${btn('settings', 'Сохранить')}${btn('subs', 'Обновить подписки')}</div>`)}
    </div>
    ${card('Конфигурация mihomo (YAML или JSON)', `<p class="hint">Контроллер, секрет и веб-интерфейс панель добавляет сама. Если в конфигурации нет секции <span class="mono">tun:</span>, она будет добавлена.</p>
      <textarea class="mono" id="mConf" style="min-height:420px">${esc(conf.content)}</textarea><div class="zm-actions">${btn('saveconf', 'Сохранить', 'pri')}</div>`)}
    ${serversCard()}`;
  },
  act: Object.assign({}, rtActs, {
    gen() { ZM.call('rt_mixomo_generate', { mixomo: { sub_url: val('mSub'), filter: val('mF'), list_preset: checked('mpre').join(','), warp_proxy: val('mW') } }, { title: 'Mixomo' }); },
    onoff(b) { ZM.call('rt_mixomo_settings', { mixomo: { enabled: b.checked, auto_restart: val('mAR') } }, { ok: b.checked ? 'Mixomo включён' : 'Mixomo выключен' }); },
    settings() { ZM.call('rt_mixomo_settings', { mixomo: { enabled: val('mOn'), auto_restart: val('mAR') } }, { ok: 'Сохранено' }); },
    subs() { ZM.call('rt_action', { action: 'refresh_subs' }, { ok: 'Подписки обновляются' }); },
    saveconf() { ZM.call('rt_mixomo_set', { content: val('mConf') }, { ok: 'Конфигурация сохранена' }); },
  }),
});
