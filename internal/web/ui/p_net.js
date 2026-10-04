'use strict';

// ---------------- hosts ----------------
ZM.page('hosts', {
  title: 'Hosts',
  async render() {
    const s = await ZM.api('hosts_status');
    const items = s.items.map(i => tog('h_' + i.id, esc(i.title), `${i.count} записей`, i.enabled, 'htog', { id: i.id })).join('');
    return `${s.geohide ? notice('info', `hosts заменён списком GeoHide DNS (${esc(s.geohide.toUpperCase())}) — блоки ниже могут не совпадать.`) : ''}
    <div class="zm-cards c2">
      ${card('Готовые блоки', `<p class="hint">Адреса сервисов, у которых по DNS приходит блокировка или заглушка. Каждый домен пишется отдельной строкой (Windows читает не больше 9 имён в строке).</p>${items}`)}
      <div style="display:flex;flex-direction:column;gap:14px">
      ${card('Файл hosts', `${row('Путь', `<span class="mono">${esc(s.path)}</span>`)}${row('Размер', `${bytes(s.size)}, строк: ${s.lines}`)}
        <div class="zm-actions">${btn('edit', 'Редактировать')}${btn('reset', 'Вернуть стандартный', 'dng')}</div>
        <div id="hed"></div>
        <p class="hint">Если антивирус блокирует запись hosts, добавьте исключение для папки Zapret Manager.</p>`)}
      ${card('GeoHide DNS', `<p class="hint">Полностью заменит hosts списком GeoHide (сотни сервисов через их прокси-адреса). Ваши записи и блоки выше пропадут.</p>
        <div class="grid-btns">${btn('geo', 'Россия', '', { r: 'ru' })}${btn('geo', 'Европа', '', { r: 'eu' })}${btn('geo', 'США', '', { r: 'us' })}</div>`)}
      </div>
    </div>`;
  },
  act: {
    htog(b) { ZM.call('hosts_toggle', { id: b.dataset.id }, { ok: b.checked ? 'Блок добавлен' : 'Блок убран' }); },
    async edit() {
      const box = document.getElementById('hed');
      if (box.innerHTML) { box.innerHTML = ''; return; }
      const r = await ZM.api('hosts_file_get');
      box.innerHTML = `<textarea class="mono" id="htext" style="min-height:320px">${esc(r.content)}</textarea><div class="zm-actions">${btn('save', 'Сохранить', 'pri')}</div>`;
    },
    save() { ZM.call('hosts_file_set', { content: val('htext') }, { ok: 'hosts сохранён, кэш DNS сброшен' }); },
    async reset() { if (await ZM.confirm('Вернуть стандартный hosts?', 'Будут удалены все блоки, GeoHide и ваши записи.', 'Вернуть', true)) ZM.call('hosts_reset', {}, { ok: 'hosts восстановлен' }); },
    async geo(b) { if (await ZM.confirm('Заменить hosts на GeoHide?', 'Файл будет полностью заменён. Вернуть обычный можно кнопкой «Вернуть стандартный».', 'Заменить', true)) ZM.call('hosts_geohide', { region: b.dataset.r }, { ok: 'hosts заменён на GeoHide' }); },
  },
});

// ---------------- DNS over HTTPS ----------------
ZM.page('doh', {
  title: 'DNS over HTTPS',
  dot: d => d && d.doh.installed ? (d.doh.running ? 'ok' : 'bad') : '',
  async render() {
    const s = await ZM.api('doh_status', { adapters: true });
    const prov = s.providers.map(p => `<button class="btn ${s.installed && s.current === p.id ? 'on' : ''}" data-act="prov" data-id="${p.id}">${esc(p.title)}</button>`).join('')
      + `<button class="btn ${s.installed && s.current === 'custom' ? 'on' : ''}" data-act="custom">Свой адрес…</button>`;
    const ad = (s.adapters || []).map(a => `<tr><td>${esc(a.name)}<div class="muted" style="font-size:12px">${esc(a.desc)}</div></td><td class="mono">${esc(a.current || '—')}</td><td class="muted">${a.static4 || a.static6 ? 'вручную' : 'DHCP'}</td></tr>`).join('');
    return `${s.error ? notice('bad', esc(s.error)) : ''}${s.hosts_extra ? notice('warn', 'В hosts есть свои записи — для этих доменов Windows не спрашивает DNS.') : ''}
    <div class="zm-cards c2">
      ${card('DNS over HTTPS', `
        ${tog('dohOn', 'Шифровать DNS', 'Все запросы Windows идут к выбранному провайдеру по HTTPS — провайдер интернета их не видит и не подменяет', s.installed, 'enable')}
        <hr class="zm-sep"><b>Провайдер</b><div class="grid-btns" style="margin-top:8px">${prov}</div>
        <p class="hint">Сейчас: <span class="mono">${esc(s.url)}</span></p>
        <div class="zm-row">${btn('test', 'Проверить')}<input type="text" id="tname" value="youtube.com" style="width:180px"><span id="tres" class="muted"></span></div>`,
        s.installed ? (s.running ? badge('ok', 'работает') : badge('bad', 'не работает')) : badge('off', 'выключен'))}
      ${card('Режим', `
        <div class="grid-btns">${btn('mode', 'Свой DoH-прокси', s.mode === 'proxy' ? 'on' : '', { m: 'proxy' })}${btn('mode', 'Встроенный DoH Windows 11', s.mode === 'windows' ? 'on' : '', { m: 'windows' })}</div>
        <p class="hint">${s.mode === 'proxy' ? 'Zapret Manager слушает 127.0.0.1:53 и пересылает запросы по DoH (как https-dns-proxy на роутере). Работает на Windows 10 и 11, со всеми провайдерами. При остановке службы DNS адаптеров возвращаются.' : 'Windows 11 сама шифрует DNS (netsh dns add encryption). Без промежуточного прокси, но только для провайдеров с известным IP.'}</p>
        ${tog('force', 'Принудительно', 'Заблокировать обычный DNS (порт 53) в брандмауэре — программы с «зашитым» DNS тоже пойдут через DoH', s.force_dns, 'force')}
        <hr class="zm-sep">${field('Bootstrap DNS', `<input type="text" id="boot" value="${esc(s.bootstrap_custom)}" placeholder="${esc((s.bootstrap || []).join(', '))}">`)}
        <p class="hint">Обычный DNS, через который один раз узнаётся адрес самого DoH-сервера. Пусто — адреса провайдера. Если недоступен, используется DoH по IP (1.1.1.1, 8.8.8.8).</p>
        <div class="zm-actions">${btn('boot', 'Сохранить')}</div>`)}
    </div>
    ${s.mode === 'proxy' && s.installed ? card('Статистика прокси', `<div class="kv"><span>Запросов</span><span>${s.stats.queries}</span><span>Из кэша</span><span>${s.stats.cache_hits}</span><span>Ошибок</span><span>${s.stats.errors}${s.stats.last_error ? ` <span class="muted">(${esc(s.stats.last_error)})</span>` : ''}</span><span>IP сервера</span><span class="mono">${esc(s.stats.server_ips || '—')}</span></div>`) : ''}
    ${card('Сетевые адаптеры', ad ? `<div class="scroll"><table class="zm"><tr><th>Адаптер</th><th>DNS сейчас</th><th>Исходно</th></tr>${ad}</table></div>` : '<p class="empty">Нет активных адаптеров</p>')}`;
  },
  act: {
    enable(b) { ZM.call('doh_enable', { on: b.checked }, { ok: b.checked ? 'DoH включён' : 'DoH выключен, DNS адаптеров возвращён' }); },
    prov(b) { ZM.call('doh_set', { provider: b.dataset.id }, { ok: 'Провайдер выбран' }); },
    async custom() { const u = await ZM.prompt('Свой DoH-сервер', 'Адрес вида https://сервер/dns-query', 'https://'); if (u) ZM.call('doh_set', { provider: 'custom', custom: u }, { ok: 'Готово' }); },
    mode(b) { ZM.call('doh_mode', { mode: b.dataset.m }, { ok: 'Режим изменён' }); },
    force(b) { ZM.call('doh_force', { on: b.checked }, { ok: 'Сохранено' }); },
    boot() { ZM.call('doh_bootstrap', { value: val('boot') }, { ok: 'Сохранено' }); },
    async test(b) {
      const out = document.getElementById('tres'); out.innerHTML = '<span class="spin"></span>';
      try { const r = await ZM.api('doh_test', { name: val('tname') }); out.innerHTML = `<span class="mono">${esc(r.ips.join(', '))}</span> · ${r.ms} мс`; }
      catch (e) { out.innerHTML = `<span style="color:var(--bad)">${esc(e.message)}</span>`; }
    },
  },
});

// ---------------- AmneziaWG ----------------
ZM.page('awg', {
  title: 'AmneziaWG',
  dot: d => d && (d.awg || []).some(t => t.running) ? 'ok' : '',
  async render() {
    const s = await ZM.api('awg_status');
    const tun = (s.tunnels || []).map(t => `<tr>
      <td><b>${esc(t.name)}</b>${t.warp ? ' ' + badge('off', 'WARP') : ''}${t.obfs ? ' <span class="muted" style="font-size:12px">маскировка</span>' : ''}</td>
      <td>${t.running ? badge('ok', t.handshake ? 'связь есть' : 'поднят') : badge('off', 'выключен')}</td>
      <td class="mono" style="font-size:12px">${esc(t.endpoint)}<br><span class="muted">${esc(t.address)}</span></td>
      <td class="nowrap">${t.running ? btn('tun', 'Стоп', 'sm', { n: t.name, a: 'stop' }) : btn('tun', 'Старт', 'sm pri', { n: t.name, a: 'start' })}
        ${btn('edit', 'Изменить', 'sm', { n: t.name })}${btn('tun', t.full_route ? 'Только подсеть' : 'Весь трафик', 'sm', { n: t.name, a: t.full_route ? 'full_off' : 'full_on' })}${btn('del', '✕', 'sm dng', { n: t.name })}</td></tr>`).join('');
    return `${!s.client ? notice('warn', 'Клиент AmneziaWG для Windows не установлен — туннели не поднимутся. Установите его кнопкой ниже.') : ''}
    ${card('Туннели', tun ? `<div class="scroll"><table class="zm"><tr><th>Имя</th><th>Состояние</th><th>Сервер / адрес</th><th></th></tr>${tun}</table></div>
      <p class="hint">«Весь трафик» — AllowedIPs 0.0.0.0/0: весь интернет компьютера пойдёт в туннель. Чтобы через туннель шли только выбранные сайты, оставьте «Только подсеть» и выберите туннель как интерфейс в Forkozz.</p>` : '<p class="empty">Туннелей пока нет — добавьте свой .conf или создайте WARP</p>',
      `${btn('add', '+ Добавить', 'sm pri')}${s.client ? btn('client', 'Удалить клиент', 'sm dng', { a: 'remove' }) : btn('client', 'Установить клиент', 'sm pri', { a: 'install' })}`)}
    ${card('Бесплатный WARP с маскировкой', `
      <p class="hint">Ключи Cloudflare WARP + обфускация AmneziaWG (Jc/Jmin/Jmax и сигнатурный пакет I1), как на роутере. Туннель «warp» используется Steer и Mixomo.</p>
      <div class="row2">
        ${field('Точка входа', sel('wEp', [['auto', 'Подобрать автоматически'], ...s.endpoints], 'auto'))}
        ${field('Маскировка I1', sel('wI1', s.i1_kinds.map(k => [k, { quic: 'QUIC (по умолчанию)', quic2: 'QUIC 2', dns: 'DNS-запрос', stun: 'STUN', icloud: 'iCloud DNS', sip: 'SIP', none: 'без I1' }[k] || k]), 'quic'))}
      </div>
      <label class="chk"><input type="checkbox" id="wFull" checked> Весь трафик через WARP</label>
      <label class="chk" style="margin-left:16px"><input type="checkbox" id="wNew"> Получить новые ключи</label>
      <div class="zm-actions">${btn('warp', s.warp ? 'Пересоздать WARP' : 'Создать WARP', 'pri')}</div>
      <p class="hint">Автоподбор по очереди поднимает туннель на каждой точке входа и ждёт рукопожатия.</p>`)}`;
  },
  act: {
    client(b) { ZM.call('awg_client', { action: b.dataset.a }, { title: 'AmneziaWG' }); },
    tun(b) { ZM.call('awg_tunnel', { name: b.dataset.n, action: b.dataset.a }, { ok: 'Готово' }); },
    async del(b) { if (await ZM.confirm('Удалить туннель ' + b.dataset.n + '?', 'Конфигурация будет удалена.', 'Удалить', true)) ZM.call('awg_tunnel', { name: b.dataset.n, action: 'delete' }, { ok: 'Удалён' }); },
    async edit(b) { const r = await ZM.api('awg_conf_get', { name: b.dataset.n }); this.dlg(b.dataset.n, r.content); },
    add() { this.dlg('', ''); },
    warp() { ZM.call('warp_generate', { opts: { endpoint: val('wEp'), i1: val('wI1'), full: val('wFull'), new_keys: val('wNew') } }, { title: 'WARP' }); },
  },
  dlg(name, conf) {
    const d = ZM.modal(`<h3>${name ? 'Туннель ' + esc(name) : 'Новый туннель'}</h3>
      ${field('Имя (латиница, без пробелов)', `<input type="text" id="tn" value="${esc(name)}" ${name ? 'readonly' : ''}>`)}
      ${field('Конфигурация (.conf AmneziaWG или WireGuard)', `<textarea class="mono" id="tc" style="min-height:300px" placeholder="[Interface]\nPrivateKey = …\nAddress = …\n\n[Peer]\nPublicKey = …\nEndpoint = …">${esc(conf)}</textarea>`)}
      <p class="hint">В нём закрытый ключ — никому не показывайте.</p>
      <div class="zm-actions"><button class="btn" data-x="no">Отмена</button><button class="btn pri" data-x="yes">Сохранить</button></div>`);
    d.addEventListener('click', async e => {
      const x = e.target.dataset.x; if (!x) return;
      if (x === 'no') return ZM.closeModal();
      const n = d.querySelector('#tn').value.trim(), c = d.querySelector('#tc').value;
      ZM.closeModal();
      await ZM.call('awg_conf_set', { name: n, content: c }, { ok: 'Туннель сохранён' });
    });
  },
});

// ---------------- TG WS Proxy ----------------
ZM.page('tgproxy', {
  title: 'TG WS Proxy',
  dot: d => d && (d.tg || []).some(t => t.running) ? 'ok' : (d && (d.tg || []).some(t => t.config.installed) ? 'bad' : ''),
  async render() {
    const s = await ZM.api('tg_status');
    return `<p class="zm-lead">Прокси для Telegram, которые ходят к серверам через WebSocket — работает, когда прямые подключения Telegram заблокированы. Ссылку можно открыть на этом компьютере или (с доступом из сети) на телефоне.</p>
    <div class="zm-cards c2">${s.items.map(t => this.item(t, s.lan_ip)).join('')}</div>`;
  },
  item(t, lan) {
    const c = t.config;
    if (!c.installed) return card(esc(t.title), `<p class="hint">${t.id === 'go' ? 'Go-версия: SOCKS5 (по умолчанию порт 1080) или MTProto.' : 'Rust-версия: MTProto с маскировкой FakeTLS (порт 1443).'}</p>
      <div class="zm-actions">${btn('install', 'Установить', 'pri', { id: t.id })}</div>`, badge('off', 'не установлен'));
    return card(esc(t.title) + ` <span class="muted" style="font:400 12px var(--font)">${esc(c.version)}</span>`, `
      ${t.conflict ? notice('warn', esc(t.conflict)) : ''}
      ${t.newer ? notice('info', `Доступна версия ${esc(t.latest)}`) : ''}
      ${row('Ссылка', `<a href="${esc(t.link)}" class="mono" style="font-size:12px">${esc(t.link.replace(/secret=([0-9a-f]{6})[0-9a-f]+/i, 'secret=$1…'))}</a>`)}
      <div class="zm-actions">${btn('copy', 'Копировать ссылку', 'sm', { l: t.link })}<a class="btn sm" href="${esc(t.link)}">Открыть в Telegram</a></div>
      ${row('Процесс', procInfo(t.proc) || '—')}
      <hr class="zm-sep">
      <div class="row2">
        ${field('Порт', `<input type="number" id="p_${t.id}" value="${c.port}">`)}
        ${t.id === 'go' ? field('Режим', sel('m_' + t.id, [['socks5', 'SOCKS5'], ['mtproto', 'MTProto']], c.mode)) : field('Домен FakeTLS', `<input type="text" id="f_${t.id}" value="${esc(c.faketls)}" placeholder="например, www.google.com">`)}
      </div>
      ${field('Secret', `<div class="zm-row" style="margin:0"><span class="mono secret" style="font-size:12px">${esc(c.secret)}</span>${btn('act', 'Новый', 'sm', { id: t.id, a: 'regen' })}</div>`)}
      ${t.id === 'go' ? `<div class="row2">${field('Логин SOCKS5', `<input type="text" id="u_${t.id}" value="${esc(c.user)}">`)}${field('Пароль', `<input type="password" id="pw_${t.id}" placeholder="без изменений">`)}</div>` : ''}
      <label class="chk"><input type="checkbox" id="l_${t.id}" ${c.lan ? 'checked' : ''}> Доступ из локальной сети (${esc(lan)})</label><br>
      <label class="chk"><input type="checkbox" id="c_${t.id}" ${c.cf_default ? 'checked' : ''}> Резерв через Cloudflare</label>
      ${field('Перезапускать каждые (мин, 0 — нет)', `<input type="number" id="a_${t.id}" value="${c.auto_min}" min="0" style="width:120px">`)}
      <div class="zm-actions">${btn('save', 'Сохранить', 'pri', { id: t.id })}
        ${t.running ? btn('act', 'Остановить', '', { id: t.id, a: 'stop' }) + btn('act', 'Перезапустить', '', { id: t.id, a: 'restart' }) : btn('act', 'Запустить', '', { id: t.id, a: 'start' })}
        ${btn('log', 'Журнал', '', { id: t.id })}${t.newer ? btn('install', 'Обновить', '', { id: t.id }) : ''}<span class="right"></span>${btn('remove', 'Удалить', 'dng', { id: t.id })}</div>`,
      stBadge(true, t.running));
  },
  act: {
    install(b) { ZM.call('tg_install', { id: b.dataset.id }, { title: 'TG WS Proxy' }); },
    async remove(b) { if (await ZM.confirm('Удалить прокси?', '', 'Удалить', true)) ZM.call('tg_remove', { id: b.dataset.id }, { title: 'TG WS Proxy' }); },
    act(b) { ZM.call('tg_action', { id: b.dataset.id, action: b.dataset.a }, { ok: 'Готово' }); },
    copy(b) { copy(b.dataset.l); },
    log(b) { logViewer('tg-' + b.dataset.id); },
    save(b) {
      const id = b.dataset.id;
      ZM.call('tg_config', { id, config: { port: +val('p_' + id), mode: val('m_' + id) || '', faketls: val('f_' + id) || '', user: val('u_' + id) || '', pass: val('pw_' + id) || '', lan: val('l_' + id), cf_default: val('c_' + id), auto_min: +val('a_' + id) || 0 } }, { ok: 'Сохранено, прокси перезапущен' });
    },
  },
});

// ---------------- ByeTube ----------------
ZM.page('bytetube', {
  title: 'ByeTube',
  dot: d => d && d.bytetube.installed ? (d.bytetube.running ? 'ok' : 'bad') : '',
  async render() {
    const s = await ZM.api('bt_status');
    if (!s.installed) return card('ByeTube', `<p>YouTube через <b>ByeDPI</b> (ciadpi.exe): локальный SOCKS5-прокси, через который по PAC-файлу идут только домены YouTube. Остальной трафик не трогается.</p>
      <p class="hint">Если YouTube уже работает через Zapret, ByeTube не нужен. Если нет — попробуйте его: другой способ обхода иногда срабатывает там, где winws не справляется.</p>
      <div class="zm-actions">${btn('install', 'Установить ByeTube', 'pri')}</div>`, badge('off', 'не установлен'));
    const c = s.config, res = await ZM.api('bt_results');
    const preset = (s.presets.find(p => p.Opts === c.opts) || {}).ID || 'custom';
    const rows = (res.rows || []).slice(0, 15).map((r, i) => `<tr class="${i === 0 && r.ok ? 'best' : ''}"><td class="mono" style="font-size:12px">${esc(r.opts)}</td><td class="nowrap"><b>${r.ok}</b>/${r.total}</td><td>${btn('use', 'Применить', 'sm', { o: r.opts })}</td></tr>`).join('');
    return `<div class="zm-cards c2">
      ${card('ByeTube ' + esc(c.version), `${row('Прокси', `<span class="mono">127.0.0.1:${c.port}</span>`)}${row('Доменов', s.domains)}${row('PAC', `<a class="mono" href="${esc(s.pac_url)}" target="_blank">${esc(s.pac_url)}</a>`)}${row('Процесс', procInfo(s.proc) || '—')}
        <div class="zm-actions">${s.running ? btn('stop', 'Остановить') : btn('start', 'Запустить', 'pri')}${btn('log', 'Журнал')}${btn('install', 'Обновить')}<span class="right"></span>${btn('remove', 'Удалить', 'dng')}</div>`, stBadge(true, s.running))}
      ${card('Стратегия ByeDPI', `
        ${field('Готовая стратегия', sel('bPreset', [...s.presets.map(p => [p.ID, p.Label + ' · ' + p.Name]), ['custom', 'Своя']], preset, 'preset'))}
        ${field('Параметры ciadpi', `<input type="text" id="bOpts" class="mono" style="width:100%" value="${esc(c.opts)}">`)}
        ${field('Порт', `<input type="number" id="bPort" value="${c.port}" style="width:120px">`)}
        <label class="chk"><input type="checkbox" id="bPac" ${c.pac ? 'checked' : ''}> Направлять YouTube через ByeDPI (системный PAC)</label>`)}
    </div>
    <div class="zm-cards c2">
      ${card('Домены', `<label class="chk"><input type="checkbox" id="bDef" ${c.default_domains ? 'checked' : ''}> Встроенный список (${s.default_list.length} доменов YouTube и Google)</label>
        ${field('Свои домены (по одному в строке, можно ссылки)', `<textarea class="mono" id="bCustom" style="min-height:120px">${esc(c.custom_domains)}</textarea>`)}
        <div class="zm-actions">${btn('save', 'Сохранить', 'pri')}</div>`)}
      ${card('Подбор стратегии', `<p class="hint">Каждая стратегия запускается во временном экземпляре ByeDPI на отдельном порту — основной не затрагивается — и проверяются адреса YouTube.</p>
        ${field('Дополнительные стратегии (по одной в строке)', `<textarea class="mono" id="bExtra" style="min-height:70px"></textarea>`)}
        <div class="zm-actions">${btn('test', 'Запустить подбор', 'pri')}</div>
        ${rows ? `<div class="scroll"><table class="zm"><tr><th>Параметры</th><th>Открылось</th><th></th></tr>${rows}</table></div>` : ''}`)}
    </div>`;
  },
  cfg(opts) { return { port: +val('bPort'), opts: opts || val('bOpts'), default_domains: val('bDef'), custom_domains: val('bCustom'), pac: val('bPac') }; },
  act: {
    install() { ZM.call('bt_action', { action: 'install' }, { title: 'ByeTube' }); },
    async remove() { if (await ZM.confirm('Удалить ByeTube?', 'Системный прокси (PAC) будет возвращён.', 'Удалить', true)) ZM.call('bt_action', { action: 'remove' }, { title: 'ByeTube' }); },
    start() { ZM.call('bt_action', { action: 'start' }, { ok: 'Запущен' }); },
    stop() { ZM.call('bt_action', { action: 'stop' }, { ok: 'Остановлен' }); },
    log() { logViewer('byedpi'); },
    preset(b) { const p = (this._presets || []).find(x => x.ID === b.value); if (b.value !== 'custom') ZM.api('bt_status').then(s => { const q = s.presets.find(x => x.ID === b.value); if (q) document.getElementById('bOpts').value = q.Opts; }); },
    save() { ZM.call('bt_config', { config: this.cfg() }, { ok: 'Сохранено' }); },
    use(b) { ZM.call('bt_config', { config: this.cfg(b.dataset.o) }, { ok: 'Стратегия применена' }); },
    test() { ZM.call('bt_test', { extra: val('bExtra') }, { title: 'Подбор стратегии ByeDPI' }); },
  },
});
