'use strict';

ZM.page('system', {
  title: 'Система',
  async render() {
    const [s, i] = await Promise.all([ZM.api('system_status'), ZM.api('sysinfo')]);
    const skins = [['auto', 'Claude · как в системе'], ['light', 'Claude · светлый'], ['dark', 'Claude · тёмный'], ['ultrakill', 'ULTRAKILL']];
    return `<div class="zm-cards c2">
      ${card('Компьютер', `<div class="kv">
        <span>Система</span><span>${esc(i.os)} ${esc(i.build)}</span><span>Имя</span><span>${esc(i.host)}</span>
        <span>Время</span><span>${esc(s.time)}</span><span>Работает</span><span>${dur(i.uptime)}</span>
        <span>Папка данных</span><span class="mono">${esc(i.base)}</span><span>Панель</span><span class="mono">http://127.0.0.1:${s.port}</span>
        <span>Служба</span><span>${s.service ? badge('ok', 'установлена, автозапуск') : badge('warn', 'не установлена')}</span></div>
        <div class="zm-actions">${btn('conn', 'Проверить связь')}${btn('time', 'Синхронизировать время')}<span id="connRes" class="muted"></span></div>`)}
      ${card('Сеть и Zapret', `
        ${tog('q', 'Блокировать QUIC (UDP 443 и 80)', 'Браузеры перейдут с QUIC на TCP, который обрабатывает Zapret. Правила «ZM Block…» в брандмауэре Windows', s.quic_blocked, 'quic')}
        ${tog('v6', 'IPv6 в Zapret', 'Включайте, только если провайдер даёт рабочий IPv6', s.ipv6_enabled, 'ipv6')}
        ${tog('ex', 'Expert mode', 'Разрешает одновременно Zapret и Zapret2 и прочие опасные сочетания', s.expert_mode, 'expert')}`)}
      ${card('Оформление', `${field('Стиль интерфейса', sel('skin', skins, s.theme || 'auto', 'skin'))}
        <p class="hint">ULTRAKILL — кровь, брызги при каждом действии, тряска, шкала стиля D → ULTRAKILL и HUD здоровья.</p>
        ${s.theme === 'ultrakill' ? await artSlots() : ''}`)}
      ${card('Зеркало GitHub', `<p class="hint">Если github.com и raw.githubusercontent.com открываются медленно или не открываются, загрузки пойдут через прокси-зеркало.</p>
        ${field('Источник загрузок', sel('mir', s.mirrors.map(m => [m.ID, m.Name]), s.mirror))}
        <div class="zm-actions">${btn('mirror', 'Применить')}</div>`)}
    </div>
    ${card('Версии компонентов', `<div id="vers">${ZM.versCache ? versTable(ZM.versCache) : '<p class="hint">Нажмите «Проверить», чтобы сравнить с последними релизами.</p>'}</div>`, btn('vers', 'Проверить', 'sm'))}
    ${card('Журналы', `<div class="grid-btns">${[['manager', 'Zapret Manager'], ['winws', 'winws'], ['winws2', 'winws2'], ['mihomo', 'Mihomo'], ['byedpi', 'ByeDPI'], ['tg-go', 'TG (Go)'], ['tg-rs', 'TG (Rust)']].map(([n, t]) => btn('log', t, '', { n })).join('')}</div>`)}
    ${card('Удаление', `<p>Удалит Zapret Manager и все компоненты, вернёт DNS адаптеров, hosts-блоки останутся, правила брандмауэра и системный прокси будут убраны.</p>
      <div class="zm-actions">${btn('uninstall', 'Удалить Zapret Manager полностью', 'dng')}</div>`)}`;
  },
  act: {
    async cdn() { const r = await ZM.call('skin_steam_cdn', {}, { reload: false }); if (r) { ZM.toast(`Скачано артов: ${r.imported}`, 'ok'); await UK.loadArt(); ZM.refresh(); } },
    async steam() { const r = await ZM.call('skin_steam_import', {}, { reload: false }); if (r) { ZM.toast(`Артов взято из Steam: ${r.imported}`, 'ok'); await UK.loadArt(); ZM.refresh(); } },
    pick(b) { const i = document.createElement('input'); i.type = 'file'; i.accept = 'image/*'; i.onchange = () => {
      const f = i.files[0]; if (!f) return;
      if (f.size > 6 * 1024 * 1024) return ZM.toast('Картинка больше 6 МБ', 'err');
      const rd = new FileReader();
      rd.onload = async () => { if (await ZM.call('skin_upload', { skin: 'ultrakill', slot: b.dataset.slot, data: rd.result }, { ok: 'Картинка загружена', reload: false })) { await UK.loadArt(); ZM.refresh(); } };
      rd.readAsDataURL(f); }; i.click(); },
    async unart(b) { if (await ZM.call('skin_remove', { skin: 'ultrakill', slot: b.dataset.slot }, { ok: 'Убрано', reload: false })) { await UK.loadArt(); ZM.refresh(); } },
    async conn() {
      const o = document.getElementById('connRes'); o.innerHTML = '<span class="spin"></span>';
      const c = await ZM.api('connectivity').catch(() => null);
      o.innerHTML = c ? `IPv4: ${c.ipv4_ok ? c.ipv4_ms + ' мс' : 'нет'} · IPv6: ${c.ipv6_ok ? c.ipv6_ms + ' мс' : 'нет'}` : 'ошибка';
    },
    time() { ZM.call('time_sync', {}, { ok: 'Время синхронизировано' }); },
    quic(b) { ZM.call('quic_toggle', {}, { ok: b.checked ? 'QUIC заблокирован' : 'QUIC разблокирован' }); },
    ipv6() { ZM.call('ipv6_toggle', {}, { ok: 'Готово' }); },
    expert() { ZM.call('expert_toggle', {}, { ok: 'Готово' }); },
    async skin(b) { ZM.themeSet = true; ZM.applyTheme(b.value); await ZM.api('ui_theme_set', { theme: b.value }).catch(() => {}); ZM.refresh(); },
    mirror() { ZM.call('mirror_set', { id: val('mir') }, { ok: 'Источник загрузок изменён' }); },
    log(b) { logViewer(b.dataset.n); },
    async vers(b) { b.disabled = true; try { const r = await ZM.api('versions', { refresh: true }); ZM.versCache = r.items; document.getElementById('vers').innerHTML = versTable(r.items); } catch (e) { ZM.toast(e.message, 'err'); } b.disabled = false; },
    async uninstall() {
      if (!await ZM.confirm('Удалить Zapret Manager?', 'Будут остановлены и удалены Zapret, Zapret2, прокси, Mihomo и служба. Настройки сети вернутся к исходным.', 'Удалить всё', true)) return;
      if (!await ZM.confirm('Точно?', 'Отменить это нельзя.', 'Да, удалить', true)) return;
      ZM.call('uninstall_all', {}, { title: 'Удаление Zapret Manager' });
    },
  },
});

const ART_NAMES = { bg: 'Фон', portrait: 'Портрет (боковая панель)', logo: 'Логотип', header: 'Шапка', rank_d: 'Ранг D', rank_c: 'Ранг C', rank_b: 'Ранг B', rank_a: 'Ранг A',
  rank_s: 'Ранг S', rank_ss: 'Ранг SS', rank_sss: 'Ранг SSS', rank_ultrakill: 'Ранг ULTRAKILL' };
async function artSlots() {
  const r = await ZM.api('skin_list', { skin: 'ultrakill' }).catch(() => ({ slots: [], have: {} }));
  const v = Date.now();
  return `<hr class="zm-sep"><b>Арты ULTRAKILL</b>
    <p class="hint">Официальные арты игры скачиваются на этот ПК из Steam (или берутся из вашей библиотеки), можно загрузить и свои. В программу и репозиторий они не входят.</p>
    <div class="zm-actions" style="margin-top:6px">${btn('cdn', 'Скачать арты игры из Steam', 'pri')}${btn('steam', 'Взять из установленного Steam')}</div>
    <p class="hint">Без картинок скин использует встроенные оригинальные арты Zapret Manager.</p>
    <div class="tiles" style="margin-top:12px">${r.slots.map(sl => `<div class="tile" style="cursor:default">
      <span class="tt">${esc(ART_NAMES[sl] || sl)}</span>
      ${r.have[sl] ? `<div style="height:70px;background:url('/skin/ultrakill/${sl}?v=${v}') center/contain no-repeat"></div>` : '<span class="ts">не задано</span>'}
      <span>${btn('pick', r.have[sl] ? 'Заменить' : 'Загрузить', 'sm', { slot: sl })}${r.have[sl] ? btn('unart', '✕', 'sm dng', { slot: sl }) : ''}</span></div>`).join('')}</div>`;
}
