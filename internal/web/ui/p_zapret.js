'use strict';

const TEST_MODES = [
  ['v_flowseal', 'v1–v10 + Flowseal', 'Все встроенные стратегии и стратегии Flowseal на заблокированных сайтах'],
  ['v', 'v1–v10', 'Только встроенные стратегии'],
  ['flowseal', 'Flowseal', 'Стратегии из zapret-discord-youtube'],
  ['youtube', 'YouTube', 'YouTube-стратегии (Yv01…) на адресах YouTube'],
  ['current', 'Текущая', 'Проверить текущую стратегию, ничего не меняя'],
  ['domain', 'Свои сайты', 'Подобрать стратегию под указанные домены'],
  ['custom', 'Свои стратегии', 'Ваш список стратегий на заблокированных сайтах'],
  ['custom_yt', 'Свои на YouTube', 'Ваш список стратегий на адресах YouTube'],
];

ZM.page('zapret', {
  title: 'Zapret',
  tab: 'str',
  dot: d => d && d.zapret.installed ? (d.zapret.running ? 'ok' : 'bad') : '',
  async render() {
    const z = this.z = await ZM.api('zapret_status');
    let head = '';
    head += conflictsBlock(z.conflicts);
    if (!z.installed) {
      return head + card('Zapret не установлен', `
        <p>Zapret — это <b>winws.exe</b> из проекта bol-van/zapret с драйвером WinDivert. Он меняет первые пакеты соединений так, что DPI провайдера не узнаёт заблокированный сайт. Устанавливается последняя версия с GitHub.</p>
        <div class="zm-actions">${btn('install', 'Установить', 'pri')}${btn('full', 'Установить и настроить (v7 + hosts + Gv1)')}</div>`);
    }
    head += card(`Zapret ${esc(z.version)}`, `
      <div class="zm-cards c2" style="gap:6px 24px">
        <div>${row('Стратегия', `<b>${esc(z.strategy || '—')}</b>`)}${row('Порты TCP', `<span class="mono">${esc(z.ports_tcp)}</span>`)}${row('Порты UDP', `<span class="mono">${esc(z.ports_udp)}</span>`)}</div>
        <div>${row('Процесс', procInfo(z.proc) || '—')}${z.nochange ? row('Защита', badge('warn', '#nochange — панель не меняет стратегию')) : ''}${z.ts_warning ? row('', '<span class="hint">В стратегии есть fooling=ts — если сайты не открываются, попробуйте другую</span>') : ''}</div>
      </div>
      <div class="zm-actions">
        ${z.running ? btn('stop', 'Остановить') : btn('start', 'Запустить', 'pri')}
        ${btn('restart', 'Перезапустить')}${btn('install', 'Обновить')}${btn('full', 'Переустановить и настроить')}
        ${btn('log', 'Журнал winws')}<span class="right"></span>${btn('remove', 'Удалить', 'dng')}
      </div>`, stBadge(true, z.running, { off: z.enabled ? 'не запущен' : 'выключен' }));
    head += tabs('zapret', [['str', 'Стратегии'], ['dg', 'Discord и игры'], ['lists', 'Списки'], ['test', 'Тест и автоподбор'], ['edit', 'Редактор']], this.tab);
    return head + await this['tab_' + this.tab](z);
  },

  async tab_str(z) {
    const [fs, yt] = await Promise.all([ZM.api('strategy_list', { kind: 'flowseal', peek: true }), ZM.api('strategy_list', { kind: 'youtube', peek: true })]);
    const cur = (z.strategy || '').split(' ');
    const vbtns = Array.from({ length: 10 }, (_, i) => `v${i + 1}`).map(v =>
      `<button class="btn ${cur.includes(v) ? 'on' : ''}" data-act="setv" data-id="${v.slice(1)}">${v}</button>`).join('');
    const fsItems = fs.items || [], ytItems = yt.items || [];
    const curYv = cur.find(x => /^Yv\d+$/.test(x)) || '';
    return `<div class="zm-cards c2">
      ${card('Встроенные стратегии v1–v10', `<p class="hint">Основной профиль для всех сайтов, кроме исключений. Блоки YouTube, Discord, игр и QUIC сохраняются при смене стратегии.</p><div class="grid-btns">${vbtns}</div>`)}
      ${card('Стратегии Flowseal', fsItems.length
        ? `<p class="hint">Готовые наборы из zapret-discord-youtube (Windows-проект Flowseal) — профили для Discord, YouTube, сайтов и игр.</p>
           <div class="zm-row">${sel('fsSel', fsItems, z.flowseal)}${btn('setfs', 'Применить', 'pri')}</div>`
        : `<p class="hint">Список ещё не загружен.</p>`, btn('fsload', fsItems.length ? 'Обновить' : 'Загрузить', 'sm'))}
      ${card('YouTube', `
        <p class="hint">Отдельный профиль для YouTube и Google (список zapret-hosts-google). Сейчас: <b>${esc(curYv || (z.yv_off ? 'выключен' : '—'))}</b></p>
        ${ytItems.length ? `<div class="zm-row">${sel('ytSel', [['off', 'Выключить'], ...ytItems], curYv)}${btn('setyt', 'Применить', 'pri')}</div>` : '<p class="hint">Список YouTube-стратегий не загружен.</p>'}
        ${tog('quic', 'QUIC для YouTube (UDP 443)', 'Браузеры часто грузят видео по QUIC — этот блок обходит блокировку и для него', z.quic_yt, 'quic')}`,
        btn('ytload', ytItems.length ? 'Обновить' : 'Загрузить', 'sm'))}
      ${card('Дополнения', `
        ${tog('rkn', `Списки РКН${z.rkn_count ? ` (${z.rkn_count} доменов)` : ''}`, z.rkn_fits ? 'Основной профиль работает только по своему списку и большому списку РКН (zapret4rocket), а не «всё, кроме исключений»' : 'Подходит только для стратегий v1–v10', z.rkn_on, 'rkn')}
        ${tog('wss', 'Блок --wssize 1:6', 'Отдельный профиль, уменьшающий TCP-окно — помогает на части провайдеров', z.wssize, 'wss')}
        ${tog('v6', 'IPv6', 'Обрабатывать и IPv6-трафик (включайте, только если IPv6 у вас работает)', z.ipv6, 'ipv6')}`)}
    </div>`;
  },

  async tab_dg() {
    const [d, g] = await Promise.all([ZM.api('discord_status'), ZM.api('game_status')]);
    const dv = d.available.map(x => [x, x]);
    return `<div class="zm-cards c2">
      ${card('Discord', `
        <p class="hint">Голос (UDP, STUN) и медиа-серверы discord.media. Сейчас: <b>${esc(d.current || (d.active ? 'включено' : 'выключено'))}</b></p>
        <div class="zm-row">${sel('dvSel', [['off', 'Выключить'], ...dv], d.current)}${btn('setdv', 'Применить', 'pri')}</div>
        ${d.active ? `<div class="zm-row"><span class="lbl">Fake для голоса</span>${sel('dvFake', d.fakes, d.current_fake)}${btn('dvfake', 'Сменить')}</div>` : ''}
        <p class="hint">Перебирайте Dv1…Dv17, если голос не подключается или «RTC Connecting».</p>`, d.active ? badge('ok', 'включено') : badge('off', 'выключено'))}
      ${card('Игры', `
        <p class="hint">UDP- и TCP-порты популярных игр (Steam, Battle.net, Riot, Minecraft…). Сейчас: <b>${esc(g.current || (g.active ? 'Flowseal' : 'выключено'))}</b>${g.xtreme ? ' · Xtreme' : ''}</p>
        <div class="grid-btns">${[1, 2, 3, 4].map(n => `<button class="btn ${g.current === 'Gv' + n ? 'on' : ''}" data-act="setgv" data-id="Gv${n}">Gv${n}</button>`).join('')}<button class="btn" data-act="setgv" data-id="off">Выключить</button></div>
        ${g.active ? `<div class="zm-row" style="margin-top:12px"><span class="lbl">Fake для UDP</span>${sel('gvFake', g.fakes, g.fake)}${btn('gvfake', 'Сменить')}</div>
        ${tog('xt', 'Xtreme', 'Расширить игровой профиль на все порты 80, 88, 444–65535 — для игр, которых нет в списке', g.xtreme, 'xtreme')}` : ''}`,
        g.active ? badge('ok', 'включено') : badge('off', 'выключено'))}
    </div>`;
  },

  async tab_lists() {
    const r = await ZM.api('lists_status');
    const names = { exclude: ['Исключения', 'Сайты, которые Zapret не трогает (банки, госуслуги…). Обновляется из репозитория StressOzz.'],
      user: ['Свой список', 'Ваши домены — используются в режиме РКН и в своих стратегиях.'],
      google: ['YouTube / Google', 'Домены для YouTube-профиля и QUIC.'] };
    return r.lists.map(l => card(`${names[l.id][0]} <span class="muted" style="font:400 13px var(--font)">${l.count} записей · ${when(l.mtime)}</span>`, `
      <p class="hint">${names[l.id][1]} <span class="mono">${esc(l.path)}</span></p>
      <div id="ed_${l.id}"></div>
      <div class="zm-actions">
        ${btn('ledit', 'Редактировать', '', { id: l.id })}
        ${l.id === 'exclude' ? btn('lrestore', 'Вернуть исходный', '', { id: l.id }) : ''}
        ${l.id === 'exclude' ? `<span class="muted">Автообновление</span>${sel('xauto', [['off', 'выкл'], ['h2', 'каждые 2 ч'], ['h4', 'каждые 4 ч'], ['h6', 'каждые 6 ч'], ['h12', 'каждые 12 ч']], l.auto, 'xauto')}` : ''}
        <label class="chk right"><input type="checkbox" data-chg="lnc" data-id="${l.id}" ${l.nochange ? 'checked' : ''}> Не изменять (#nochange)</label>
      </div>`)).join('');
  },

  async tab_test() {
    const st = await ZM.api('test_status');
    this.tmode = this.tmode || 'v_flowseal';
    const res = await ZM.api('test_results', { mode: this.tmode });
    const a = st.auto || {}, last = a.last;
    const modes = TEST_MODES.map(([id, t]) => `<button class="btn ${this.tmode === id ? 'on' : ''}" data-act="tmode" data-id="${id}">${t}${st.has[id] ? ' ✓' : ''}</button>`).join('');
    const desc = (TEST_MODES.find(m => m[0] === this.tmode) || [])[2];
    let table = '<p class="hint">Результатов этого режима пока нет.</p>';
    if (res && res.items) {
      const best = res.items.filter(i => !i.control).reduce((b, i) => !b || i.ok > b.ok ? i : b, null);
      table = `<p class="hint">Тест от ${when(res.at)}${res.stopped ? ' (остановлен)' : ''}, адресов: ${res.domains.length}</p>
      <div class="scroll"><table class="zm"><tr><th>Стратегия</th><th>Открылось</th><th>Не открылись</th><th></th></tr>${res.items.map(i => `
        <tr class="${best && i === best && i.ok > 0 ? 'best' : ''}"><td>${esc(i.name)}</td><td class="nowrap"><b>${i.ok}</b> / ${i.total}</td>
        <td class="muted" style="font-size:12px">${esc((i.fails || []).slice(0, 12).join(', '))}${(i.fails || []).length > 12 ? '…' : ''}</td>
        <td>${!i.control && canApply(this.tmode, i.name) ? btn('tapply', 'Применить', 'sm', { name: i.name }) : ''}</td></tr>`).join('')}</table></div>`;
    }
    return `${card('Тест стратегий', `
      <p class="hint">Каждая стратегия по очереди применяется, winws перезапускается и проверяются заблокированные сайты. Текущие настройки потом возвращаются.</p>
      <div class="grid-btns">${modes}</div>
      <p class="hint">${esc(desc)}</p>
      ${this.tmode === 'domain' ? field('Домены (до 30, через пробел или запятую)', `<input type="text" id="tdoms" style="width:100%" placeholder="rutracker.org, x.com">`) : ''}
      <div class="zm-actions">${st.running ? btn('tstop', 'Остановить тест', 'dng') : btn('tstart', 'Запустить тест', 'pri')}
        ${this.tmode === 'domain' ? btn('tadd', 'Добавить домены в свой список') : ''}${btn('tclear', 'Очистить результаты')}</div>
      <hr class="zm-sep">${table}`)}
    <div class="zm-cards c2">
    ${card('Автоподбор по расписанию', `
      <p class="hint">Раз в сутки тест подбирает лучшую стратегию и применяет её, только если она открыла больше сайтов, чем текущая.</p>
      <div class="zm-row"><span class="lbl">Время</span><input type="time" id="abTime" value="${esc(a.time || '04:00')}">
        ${sel('abMode', [['v_flowseal', 'v + Flowseal'], ['v', 'v1–v10'], ['flowseal', 'Flowseal']], a.mode)}</div>
      <div class="zm-actions">${btn('abOn', a.time ? 'Сохранить' : 'Включить', 'pri')}${a.time ? btn('abOff', 'Выключить') : ''}${btn('abRun', 'Запустить сейчас')}</div>
      ${row('Сейчас', a.time ? badge('ok', 'каждый день в ' + a.time) : badge('off', 'выключен'))}
      ${last ? row('Последний запуск', `${when(last.at)} — ${esc({ applied: 'применена ' + last.best, kept: 'оставлена текущая', none: 'не удалось определить', nochange: '#nochange — ничего не меняли', error: 'ошибка' }[last.result] || last.result)}${last.best ? ` <span class="muted">(лучшая ${esc(last.best)} ${last.best_ok}/${last.total}, текущая ${last.cur_ok})</span>` : ''}`) : ''}`)}
    ${card('Свои стратегии для теста', `
      <p class="hint">Каждая начинается со строки <span class="mono">#Название</span>, далее параметры winws по одному в строке. Пути можно писать как на роутере (/opt/zapret/files/fake/…).</p>
      <textarea class="mono" id="custom" placeholder="#my1&#10;--filter-tcp=443&#10;--dpi-desync=fake,multisplit">${esc(st.custom || '')}</textarea>
      <div class="zm-actions">${btn('csave', 'Сохранить', 'pri')}</div>`)}
    </div>`;
  },

  async tab_edit(z) {
    const r = await ZM.api('nfqws_opt_get');
    return `<div class="zm-cards c2">
    ${card('Текущая стратегия', `
      <p class="hint">Блок NFQWS_OPT в том же формате, что на роутере: профили разделяются <span class="mono">--new</span>, строки <span class="mono">#…</span> — названия и маркеры. Можно вставить целиком блок из /etc/config/zapret — пути переведутся автоматически.</p>
      <textarea class="mono" id="opt" style="min-height:380px">${esc(r.content)}</textarea>
      <div class="zm-actions">${btn('osave', 'Сохранить и перезапустить', 'pri')}${btn('export', 'Экспорт для роутера')}
        <label class="chk right"><input type="checkbox" data-chg="snc" ${z.nochange ? 'checked' : ''}> Не изменять стратегию (#nochange)</label></div>`)}
    ${card('Порты перехвата', `
      <p class="hint">Какие порты WinDivert передаёт в winws (на роутере — NFQWS_PORTS_TCP/UDP). Панель сама добавляет порты Discord и игр.</p>
      ${field('TCP', `<input type="text" id="ptcp" class="mono" value="${esc(z.ports_tcp)}">`)}
      ${field('UDP', `<input type="text" id="pudp" class="mono" value="${esc(z.ports_udp)}">`)}
      <div class="zm-actions">${btn('ports', 'Сохранить', 'pri')}</div>
      <hr class="zm-sep"><b>Командная строка winws</b>
      <pre class="log" style="max-height:260px">${esc(r.args)}</pre>`)}
    </div>`;
  },

  act: {
    tab(b) { this.tab = b.dataset.tab; ZM.refresh(); },
    async install() { if (await beforeZapret()) ZM.call('zapret_action', { action: 'install' }, { title: 'Установка Zapret' }); },
    async full() { if (!await beforeZapret()) return; if (await ZM.confirm('Переустановить и настроить?', 'Zapret будет удалён и поставлен заново, затем применятся v7, блоки hosts и игровая стратегия Gv1.')) ZM.call('zapret_action', { action: 'full' }, { title: 'Установка и настройка Zapret' }); },
    async remove() { if (await ZM.confirm('Удалить Zapret?', 'Будут удалены winws, стратегии, списки и результаты тестов.', 'Удалить', true)) ZM.call('zapret_action', { action: 'remove' }, { title: 'Удаление Zapret' }); },
    async start() { if (await beforeZapret()) ZM.call('zapret_action', { action: 'start' }, { ok: 'Zapret запущен' }); },
    stop() { ZM.call('zapret_action', { action: 'stop' }, { ok: 'Zapret остановлен' }); },
    async restart() { if (await beforeZapret()) ZM.call('zapret_action', { action: 'restart' }, { ok: 'Перезапущен' }); },
    log() { logViewer('winws'); },
    setv(b) { ZM.call('strategy_set', { kind: 'v', id: b.dataset.id }, { ok: 'Стратегия v' + b.dataset.id + ' применена' }); },
    setfs() { ZM.call('strategy_set', { kind: 'flowseal', id: val('fsSel') }, { ok: 'Применена ' + val('fsSel') }); },
    setyt() { ZM.call('strategy_set', { kind: 'youtube', id: val('ytSel') }, { ok: 'Готово' }); },
    fsload() { ZM.call('strategy_list', { kind: 'flowseal', refresh: true }, { title: 'Загрузка стратегий Flowseal' }); },
    ytload() { ZM.call('strategy_list', { kind: 'youtube', refresh: true }, { title: 'Загрузка YouTube-стратегий' }); },
    quic(b) { ZM.call('youtube_quic_set', { on: b.checked }, { ok: b.checked ? 'QUIC-блок добавлен' : 'QUIC-блок убран' }); },
    rkn(b) { ZM.call('opt_set', { what: 'rkn', val: b.checked ? 'on' : 'off' }, { ok: 'Готово' }); },
    wss(b) { ZM.call('opt_set', { what: 'wssize', val: b.checked ? 'on' : 'off' }, { ok: 'Готово' }); },
    ipv6() { ZM.call('ipv6_toggle', {}, { ok: 'Готово' }); },
    setdv() { ZM.call('discord_set', { num: val('dvSel') }, { ok: 'Готово' }); },
    dvfake() { ZM.call('discord_fake', { file: val('dvFake') }, { ok: 'Fake-файл сменён' }); },
    setgv(b) { ZM.call('game_set', { choice: b.dataset.id }, { ok: 'Готово' }); },
    gvfake() { ZM.call('game_fake', { file: val('gvFake') }, { ok: 'Fake-файл сменён' }); },
    xtreme() { ZM.call('game_xtreme', {}, { ok: 'Готово' }); },
    async ledit(b) {
      const id = b.dataset.id, box = document.getElementById('ed_' + id);
      if (box.innerHTML) { box.innerHTML = ''; return; }
      const r = await ZM.api('list_get', { id });
      box.innerHTML = `<textarea class="mono" id="lt_${id}" style="min-height:260px">${esc(r.content)}</textarea>
        <div class="zm-actions">${btn('lsave', 'Сохранить', 'pri', { id })}</div>`;
    },
    lsave(b) { ZM.call('list_set', { id: b.dataset.id, content: val('lt_' + b.dataset.id) }, { ok: 'Список сохранён, winws перезапущен' }); },
    async lrestore(b) { if (await ZM.confirm('Вернуть исходный список исключений?', 'Ваши правки в нём пропадут.')) ZM.call('list_restore', { id: b.dataset.id }, { ok: 'Список восстановлен' }); },
    xauto(b) { ZM.call('excl_auto', { v: b.value }, { ok: 'Сохранено' }); },
    lnc(b) { ZM.call('nochange_set', { id: b.dataset.id, on: b.checked }, { ok: 'Сохранено' }); },
    snc(b) { ZM.call('nochange_set', { id: 'strategy', on: b.checked }, { ok: b.checked ? 'Стратегия защищена от изменений' : 'Защита снята' }); },
    tmode(b) { this.tmode = b.dataset.id; ZM.refresh(); },
    tstart() { ZM.call('test_start', { mode: this.tmode, domains: val('tdoms') }, { title: 'Тест стратегий' }); },
    tstop() { ZM.call('test_stop', {}, { ok: 'Тест останавливается…' }); },
    tadd() { ZM.call('add_user_domains', { domains: val('tdoms') }, { ok: 'Домены добавлены в свой список' }); },
    tclear() { ZM.call('test_clear', { mode: this.tmode }, { ok: 'Очищено' }); },
    tapply(b) {
      const n = b.dataset.name, m = /^v(\d+)$/.exec(n);
      if (this.tmode === 'youtube') ZM.call('strategy_set', { kind: 'youtube', id: n }, { ok: 'Применена ' + n });
      else if (m) ZM.call('strategy_set', { kind: 'v', id: m[1] }, { ok: 'Применена ' + n });
      else ZM.call('strategy_set', { kind: 'flowseal', id: n }, { ok: 'Применена ' + n });
    },
    abOn() { ZM.call('autobest_set', { time: val('abTime'), mode: val('abMode') }, { ok: 'Автоподбор включён' }); },
    abOff() { ZM.call('autobest_set', { time: 'off', mode: val('abMode') }, { ok: 'Автоподбор выключен' }); },
    abRun() { ZM.call('autobest_run', {}, { title: 'Автоподбор стратегии' }); },
    async csave() { const r = await ZM.call('test_custom_set', { content: val('custom') }, { reload: false }); if (r) ZM.toast(`Сохранено стратегий: ${r.count}`, 'ok'); },
    osave() { ZM.call('nfqws_opt_set', { content: val('opt') }, { ok: 'Стратегия сохранена, winws перезапущен' }); },
    async export() { const r = await ZM.api('export_router'); ZM.modal(`<h3>Экспорт для роутера</h3><p class="hint">Вставьте в /etc/config/zapret вместо соответствующих опций.</p><pre class="log">${esc(r.text)}</pre><div class="zm-actions"><button class="btn" data-copy="${esc(r.text)}">Копировать</button><button class="btn pri" onclick="ZM.closeModal()">Закрыть</button></div>`); },
    ports() { ZM.call('ports_set', { tcp: val('ptcp'), udp: val('pudp') }, { ok: 'Порты сохранены' }); },
  },
});

function canApply(mode, name) {
  if (mode === 'current' || mode === 'custom' || mode === 'custom_yt') return false;
  if (mode === 'youtube') return /^Yv\d+$/.test(name);
  return true;
}

ZM.page('zapret2', {
  title: 'Zapret2',
  dot: d => d && d.zapret2.installed ? (d.zapret2.running ? 'ok' : 'bad') : '',
  async render() {
    const s = await ZM.api('z2_status');
    if (!s.installed) return card('Zapret2', `
      <p><b>winws2</b> — новое поколение zapret: стратегии описываются lua-программами (<span class="mono">--lua-desync</span>). Несовместим с основным Zapret: оба перехватывают один и тот же трафик.</p>
      ${s.zapret_installed && !s.expert ? notice('warn', 'Стоит Zapret — он несовместим с Zapret2. Удалите Zapret или включите Expert mode в «Системе».') : ''}
      <div class="zm-actions">${btn('install', 'Установить Zapret2', 'pri')}</div>`, badge('off', 'не установлен'));
    const opt = await ZM.api('z2_opt_get');
    return `${card('Zapret2 ' + esc(s.version), `${row('Процесс', procInfo(s.proc) || '—')}
      <div class="zm-actions">${s.running ? btn('stop', 'Остановить') : btn('start', 'Запустить', 'pri')}${btn('install', 'Обновить')}${btn('log', 'Журнал')}<span class="right"></span>${btn('remove', 'Удалить', 'dng')}</div>`, stBadge(true, s.running))}
    <div class="zm-cards c2">
      ${card('Стратегия (NFQWS2_OPT)', `<p class="hint">&lt;HOSTLIST&gt; заменяется на исключения из списков Zapret. lua-библиотеки подключаются автоматически.</p>
        <textarea class="mono" id="z2opt" style="min-height:280px">${esc(opt.content)}</textarea><div class="zm-actions">${btn('save', 'Сохранить и перезапустить', 'pri')}</div>`)}
      ${card('Порты', `${field('TCP', `<input type="text" id="z2tcp" class="mono" value="${esc(s.ports.tcp)}">`)}${field('UDP', `<input type="text" id="z2udp" class="mono" value="${esc(s.ports.udp)}">`)}
        <div class="zm-actions">${btn('ports', 'Сохранить', 'pri')}</div>`)}
    </div>`;
  },
  act: {
    install() { ZM.call('z2_action', { action: 'install' }, { title: 'Установка Zapret2' }); },
    async remove() { if (await ZM.confirm('Удалить Zapret2?', '', 'Удалить', true)) ZM.call('z2_action', { action: 'remove' }, { title: 'Удаление Zapret2' }); },
    start() { ZM.call('z2_action', { action: 'start' }, { ok: 'Запущен' }); },
    stop() { ZM.call('z2_action', { action: 'stop' }, { ok: 'Остановлен' }); },
    log() { logViewer('winws2'); },
    save() { ZM.call('z2_opt_set', { content: val('z2opt') }, { ok: 'Сохранено' }); },
    ports() { ZM.call('z2_ports_set', { tcp: val('z2tcp'), udp: val('z2udp') }, { ok: 'Сохранено' }); },
  },
});
