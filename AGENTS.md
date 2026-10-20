# AGENTS.md — как ориентироваться в этом репозитории

Инструкции для ИИ-агентов и людей, которые правят код через ИИ. Читать целиком перед
первой правкой.

## Структура: одна ветка, один Go-модуль, модули = пакеты

- Ветка одна — `main`. Веток под модули нет намеренно: приложение собирается целиком,
  а ветка-модуль сделала бы сборку невозможной без merge.
- Go-модуль один — `github.com/zapretmanager/zmwin` (см. `go.mod`).
- «Модуль» здесь = пакет в `internal/`. Подробная карта с контрактами — `docs/modules.md`.
- Зависимости идут строго вниз: `app`, `osx` — база; `evade` не зависит ни от чего внутри
  проекта; `web` — агрегатор API; `core` — сборка и планировщик. Импорт «вверх» = ошибка
  границы.

## Маршрутизация: где что менять

| Задача | Модуль | Файлы |
|---|---|---|
| Пресет стратегии (v1–v10, Dv, Gv), порты | `internal/zapret` | `data.go` |
| Сеттеры стратегий, Flowseal/YouTube/Discord/Game/Xtreme/RKN | `internal/zapret` | `strategy.go` |
| Формат `strategy.txt`, маркеры, портируемые пути | `internal/zapret` | `body.go` |
| Генерация/рандомизация стратегии, `--dpi-desync-ttl`, валидация | `internal/zapret` | `random.go` |
| Запуск `winws`, `BuildArgs`, конфликты, установка | `internal/zapret` | `zapret.go` |
| Тестер стратегий, автоподбор, метрики | `internal/zapret` | `tester.go` |
| winws2 + lua | `internal/zapret` | `zapret2.go` |
| Признаки блокировки, пробы, классификатор, план лечения | `internal/evade` | `probe.go`, `evade.go` |
| Модель (26→16→3), признаки, обучение | `internal/evade` | `model.go`, `synthetic.go`, `genmodel/main.go` |
| Переобучить и вшить веса | — | `go run ./internal/evade/genmodel` → `model_default.json` |
| API-эндпоинты панели | `internal/web` | `api.go` |
| Сервер, токен, CSRF, guard, встраивание UI | `internal/web` | `server.go` |
| Страницы панели (SPA) | `internal/web/ui` | `p_dash.js`, `p_zapret.js`, `p_net.js`, `p_route.js`, `p_sys.js`, `core.js` |
| Скин ULTRAKILL | `internal/web/ui` | `uk.js`, `style.css` |
| Служба Windows, процессы, PowerShell, firewall, Job Object | `internal/osx` | `osx_windows.go`, `proc.go` |
| Пути, `settings.json`, KV, фоновые `Job`, загрузки | `internal/app` | `paths.go`, `state.go`, `jobs.go` |
| Порядок загрузки, планировщик, `Shutdown` | `internal/core` | `core.go` |
| DNS over HTTPS, свой прокси :53 | `internal/doh` | `doh.go`, `proxy.go` |
| Hosts-пресеты, GeoHide | `internal/hosts` | `blocks.go`, `hosts.go` |
| mihomo (Steer/Forkozz/Mixomo), конфиг, rule-providers | `internal/routing` | `engine.go`, `ops.go`, `model.go` |
| TG WS Proxy | `internal/tg` | `tg.go` |
| AmneziaWG / WARP | `internal/awg` | `awg.go` |
| ByeDPI + PAC | `internal/byetube` | `byetube.go`, `lists.go` |
| Консольное меню | `internal/cli` | `menu.go` |
| Точка входа, CLI-команды, установка | `cmd/zapret-manager` | `main.go` |

Как найти место, если задача не в таблице: `go doc ./internal/<пакет>`, затем
`grep -rn "Символ" internal/`; список зависимостей пакета — `go list -deps ./internal/<пакет>`.

## Жёсткие правила проекта

1. **Совместимость с роутером.** Текст стратегии — это блок `NFQWS_OPT` (один токен на
   строку, маркеры `#name`, `--new`), пути портируемые (`files/fake/…`, `lists/…`).
   Правки в `strategy.go`/`body.go` обязаны держать зелёными `TestRouterParity` и
   `TestFlowsealParity`.
2. **Обратимость.** Любое системное изменение сохраняет прежнее состояние и откатывается:
   DNS (адаптеры), PAC (`AutoConfigURL` по SID), firewall-правила с префиксом `ZM …`,
   `UninstallAll`.
3. **Долгие операции** — только через `app.StartJob` + `j.Say("==> …")`, ошибки
   пользователю по-русски.
4. **Никакого GitHub API** (лимиты): `app.LatestTag` (редирект `/releases/latest`) и прямые
   ссылки на ассеты, загрузки через `app.Mirrored`.
5. **Одна ветка.** Не создавать ветки под модули, не делать merge-структуру; изменения —
   прямо в `main`.
6. **UI встроен** через `//go:embed ui`: после правок в `internal/web/ui` нужна пересборка.

## Проверка перед отдачей

```bash
go vet ./...                       # Windows-таргет
GOOS=linux go vet ./...            # dev-режим не должен сломаться
go test ./...                      # все пакеты
node --check internal/web/ui/*.js  # если трогали UI
./build.sh                         # dist/ZapretManager.exe
```

Тесты `internal/evade`, `internal/web` (эндпоинты `evade_*`) требуют `ZM_BASE` в путь,
куда есть запись, иначе часть тестов пишет в `%ProgramData%`:

```bash
ZM_BASE="$(cygpath -w /tmp/zmtest)" go test ./internal/evade/ ./internal/web/ -count=1
```

## Что нельзя делать

- Менять сигнатуры экспортируемых функций модуля, не поправив всех потребителей
  (`web/api.go`, `core`, `cli`).
- Класть в репозиторий артефакты сборки и игры-арты (`dist/`, `*.exe`, `*.syso` — в
  `.gitignore`).
- Ослаблять проверки в тестах, чтобы «стало зелёным»: сначала причина падения.
- Выносить пакеты из `internal/` без причины: из другого Go-модуля их импортировать
  нельзя.

## Глубже

- `docs/modules.md` — карта модулей, контракты, зависимости.
- `docs/adaptive-core.md` — классы блокировок 2026, слои ядра, границы применимости.
- `.claude/skills/zapret-manager-windows/SKILL.md` — исторический гайд по порту с роутера.
