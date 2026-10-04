---
name: zapret-manager-windows
description: Everything needed to work on Zapret Manager for Windows (Go port of StressOzz Zapret Manager for OpenWrt) — architecture, module map, router parity rules, dev/test/build workflow, UI skins and pitfalls. Load before changing any code in this repo.
---

# Zapret Manager for Windows — project guide

Single Go binary `ZapretManager.exe` = Windows service + web panel on `127.0.0.1:17580` + console menu.
Port of **StressOzz/Zapret-Manager** (OpenWrt): TUI `Zapret-Manager.sh` (`zms`) + LuCI app
(`/opt/zapret-manager-luci/backend.sh`, ~800 shell functions, views in `/www/luci-static/resources/view/zapret-manager/`).
The original router (if available) is `ssh root@192.168.1.1` — read backend.sh there to check behaviour.

## Layout

```
cmd/zapret-manager/   main.go (launcher/CLI commands), service_windows.go (svc), service_other.go
internal/app/         paths (Base=%ProgramData%\ZapretManager, ZM_BASE overrides), settings.json (app.S/Update),
                      KV json stores, background Jobs (StartJob, j.Say, log tail), downloads (Fetch, Download,
                      LatestTag via /releases/latest redirect — never the GitHub API), Unzip/UnTarGz, mirrors
internal/osx/         Windows layer: Run (hidden), PS (PowerShell UTF-8), firewall rules ("ZM ..."), process
                      supervisor ProcStart/ProcStop (restart backoff, Job Object kill-on-close), service mgmt,
                      Elevate, OpenURL. osx_other.go = dev-mode simulation on Linux.
internal/zapret/      body.go (strategy model + DropProfiles = port of _nfq_drop_profiles), data.go (v1–v10, Dv1–17,
                      Gv, ports), strategy.go (SetV/SetFlowseal/SetYoutube/Discord/Game/Xtreme/RKN/wssize/raw/nochange),
                      flowseal.go (bat parser), zapret.go (install winws, BuildArgs, Restart, status, conflicts),
                      lists.go, tester.go (strategy test + AutoBest), zapret2.go (winws2 + lua)
internal/hosts/       hosts presets (blocks.go generated from router _hosts_block), GeoHide, editor
internal/doh/         proxy.go = own DNS→DoH forwarder on 127.0.0.1:53 (UDP+TCP, cache, bootstrap w/o system DNS);
                      doh.go = providers, adapter DNS switch + restore, native Win11 DoH, force (firewall 53)
internal/tg/          TG WS Proxy: Go (socks5/mtproto) + Rust (mtproto+FakeTLS), links, auto-restart
internal/awg/         AmneziaWG client (msi), tunnel services, WARP registration (Cloudflare API) + I1 obfuscation
internal/byetube/     ByeDPI ciadpi + PAC (/byetube.pac) written to HKEY_USERS\<sid> AutoConfigURL; strategy test via SOCKS
internal/routing/     one mihomo (TUN) engine for Steer (WARP group), Forkozz (sections) and Mixomo (own config);
                      itdoginfo .lst lists converted to mihomo rule-providers ("+.domain"); controller API; explain
internal/sysinfo/     system page: info, connectivity, QUIC block, IPv6, mirror, theme, time sync
internal/core/        service boot order, scheduler (cron replacement), Shutdown (restores DNS/PAC)
internal/web/         server.go (auth/token/host guard/CSRF), api.go (all API routes), maint.go, skin.go (art slots)
internal/web/ui/      SPA (vanilla JS): core.js (ZM: api/call/jobDialog/nav/theme), p_*.js pages, uk.js (ULTRAKILL engine),
                      style.css (Claude tokens + [data-skin=ultrakill] skin), fonts/
internal/cli/         console menu (`menu`) — an API client of the running service
```

## Core rules

1. **Strategy text stays router-compatible.** The body (`state/strategy.txt`) is the NFQWS_OPT block: one token per line,
   `#name`/`#YvNN`/`#DvN`/`#GvN`/`#udp443` markers, `--new` separators. Paths are portable (`files/fake/x.bin`,
   `lists/x.txt`), made absolute only in `BuildArgs`. `NormalizePaths`/`RouterPaths` convert from/to `/opt/zapret/...`.
2. **Router parity is tested.** Any change in zapret/strategy.go or body.go must keep `TestRouterParity` green: it replays
   operation sequences through the original shell functions (harness extracted from backend.sh) and compares bytes.
   Harness: take backend.sh lines with strategy/nfq/discord/game/zo functions into lib.sh, stub zapret_restart etc.,
   redirect `/opt/zapret/tmp/GvXtreme` to `$GVX`. Flowseal parser: `TestFlowsealParity` vs router's flowseal_strategies.txt.
3. Every system change must be **reversible**: DoH saves adapters (`Saved`) and restores; ByeTube saves previous
   AutoConfigURL per SID; firewall rules are named `ZM ...`; UninstallAll undoes all.
4. Long operations → `app.StartJob` + `j.Say("==> …")`; the UI shows them via `jobDialog`. Errors are Russian, user-facing.
5. No GitHub API (rate limits): use `app.LatestTag` (redirect) and release asset URLs. Downloads go through `app.Mirrored`.
6. Dev mode (`app.Dev`, non-Windows): `.exe` processes and PowerShell are simulated, data in `./devroot` or `ZM_BASE`.
7. Never ship copyrighted game art in the repo. ULTRAKILL art = runtime download from Steam CDN / local Steam cache /
   user upload into `%ProgramData%\ZapretManager\skins\ultrakill\` (slots in web/skin.go).

## Workflow

- Vet both OSes: `go vet ./... && GOOS=windows go vet ./...`
- Build: `./build.sh` → `dist/ZapretManager.exe` (Windows x64, no cgo)
- Run panel locally: `ZM_BASE=/tmp/zmdev go run ./cmd/zapret-manager run`, open `http://127.0.0.1:17580/login?t=$(cat /tmp/zmdev/state/token)`
  (`&next=/%3Fdemo` shows the ULTRAKILL blood demo). Headless shots: chromium `--disable-features=AutoDarkMode`.
- Validate mihomo configs: `ZM_MIHOMO=<linux mihomo> go test ./internal/routing -run GenerateValid`
- Live checks (network): `ZM_LIVE=1 go test ./internal/doh ./internal/awg` (WARP API may be blocked on some networks).
- JS: `node --check internal/web/ui/*.js`. UI is embedded via `//go:embed ui` — rebuild after UI edits.

## UI

- Pages register with `ZM.page(id, {title, render(), act:{…}, dot(dash), live(dash)})`; actions via `data-act`/`data-chg`.
- Helpers in core.js: `card, btn, tog, row, sel, field, badge, stBadge, notice, tabs, logViewer, val, esc`.
- Themes: settings.Theme = auto|light|dark|ultrakill; server injects `data-theme`/`data-skin` into index.html.
- ULTRAKILL (uk.js): canvases `#ukStains` (persistent) / `#ukBlood` (fresh), `UK.hit/damage/style/pop`, style ranks
  D…ULTRAKILL with decay, HUD in header, HP in sidebar. Font "UK HUD" = VCR OSD Mono (Latin) + Pixelify Sans (Cyrillic).

## Known gaps / ideas

- Not yet verified on real Windows: WinDivert run, service install, DoH adapter switching, AmneziaWG CLI, PAC pickup.
- Original (non-game) ULTRAKILL-style art for empty slots (rank badges, background) is still to be drawn.
- Manager self-update (needs a releases repo), tray icon, exe icon/manifest (.syso via mingw windres).
- Strategy-changing API should refuse while `strategy_test` job runs.
