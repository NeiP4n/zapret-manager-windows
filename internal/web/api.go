package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/awg"
	"github.com/zapretmanager/zmwin/internal/byetube"
	"github.com/zapretmanager/zmwin/internal/doh"
	"github.com/zapretmanager/zmwin/internal/hosts"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/routing"
	"github.com/zapretmanager/zmwin/internal/sysinfo"
	"github.com/zapretmanager/zmwin/internal/tg"
	"github.com/zapretmanager/zmwin/internal/zapret"
)

type M = map[string]any

// job starts a background job and returns its handle for the UI to tail.
func job(name, label string, fn func(j *app.Job) error) (any, error) {
	if err := app.StartJob(name, label, fn); err != nil {
		return nil, fmt.Errorf("«%s» уже выполняется — дождитесь окончания", label)
	}
	return M{"job": name}, nil
}

var once sync.Once

func registerAll() { once.Do(func() { register(); registerSkin() }) }

func register() {
	// ---------- common ----------
	reg("status", func(ctx context.Context, a Args) (any, error) { return Dashboard(), nil })
	reg("sysinfo", func(ctx context.Context, a Args) (any, error) { return sysinfo.Get(), nil })
	reg("job_status", func(ctx context.Context, a Args) (any, error) { return app.JobState(a.S("job")), nil })
	reg("log_tail", func(ctx context.Context, a Args) (any, error) {
		return M{"lines": app.JobLog(a.S("job"), a.I("from")), "state": app.JobState(a.S("job"))}, nil
	})
	reg("jobs_running", func(ctx context.Context, a Args) (any, error) { return app.RunningJobs(), nil })
	reg("jobs_cancel", func(ctx context.Context, a Args) (any, error) { return M{"ok": app.CancelJob(a.S("job"))}, nil })
	reg("proc_log", func(ctx context.Context, a Args) (any, error) {
		n := a.S("name")
		if n == "manager" {
			return M{"lines": app.TailFile(app.P("logs", "manager.log"), 300)}, nil
		}
		return M{"lines": osx.TailLog(n, 300)}, nil
	})
	reg("ui_theme_set", func(ctx context.Context, a Args) (any, error) { sysinfo.SetTheme(a.S("theme")); return nil, nil })

	// ---------- Zapret ----------
	reg("zapret_status", func(ctx context.Context, a Args) (any, error) { return zapret.GetStatus(), nil })
	reg("zapret_action", func(ctx context.Context, a Args) (any, error) {
		switch a.S("action") {
		case "install", "update":
			return job("install_zapret", "Установка Zapret", zapret.Install)
		case "full":
			return job("install_zapret", "Полная переустановка Zapret", FullReinstall)
		case "remove":
			return job("remove_zapret", "Удаление Zapret", zapret.Remove)
		case "start":
			return nil, zapret.SetEnabled(true)
		case "stop":
			return nil, zapret.SetEnabled(false)
		case "restart":
			return nil, zapret.Restart()
		}
		return nil, fmt.Errorf("неизвестное действие")
	})
	reg("conflicts", func(ctx context.Context, a Args) (any, error) { return M{"items": zapret.Conflicts()}, nil })
	reg("conflicts_fix", func(ctx context.Context, a Args) (any, error) {
		done, err := zapret.FixConflicts()
		return M{"done": done}, err
	})
	reg("strategy_list", func(ctx context.Context, a Args) (any, error) {
		switch a.S("kind") {
		case "v":
			var out []M
			for i := 1; i <= 10; i++ {
				out = append(out, M{"id": fmt.Sprintf("v%d", i), "label": fmt.Sprintf("Стратегия v%d", i), "body": zapret.Join(zapret.StrategyV(i))})
			}
			return M{"items": out}, nil
		case "flowseal":
			if a.B("peek") {
				return M{"items": zapret.FlowsealList()}, nil
			}
			if a.B("refresh") || len(zapret.FlowsealList()) == 0 {
				return job("flowseal_download", "Загрузка стратегий Flowseal", zapret.DownloadFlowseal)
			}
			return M{"items": zapret.FlowsealList()}, nil
		case "youtube":
			if a.B("peek") {
				return M{"items": zapret.YoutubeList()}, nil
			}
			if a.B("refresh") || len(zapret.YoutubeList()) == 0 {
				return job("youtube_download", "Загрузка стратегий YouTube", func(j *app.Job) error { return zapret.DownloadYoutube(j.Ctx(), j) })
			}
			return M{"items": zapret.YoutubeList()}, nil
		}
		return nil, fmt.Errorf("неизвестный список")
	})
	reg("strategy_set", func(ctx context.Context, a Args) (any, error) {
		zapret.Mu.Lock()
		defer zapret.Mu.Unlock()
		var err error
		switch a.S("kind") {
		case "v":
			err = zapret.SetV(a.I("id"))
		case "flowseal":
			err = zapret.SetFlowseal(a.S("id"))
		case "youtube":
			err = zapret.SetYoutube(a.S("id"))
		default:
			err = fmt.Errorf("неизвестный тип")
		}
		if err != nil {
			return nil, err
		}
		return zapret.GetStatus(), nil
	})
	reg("youtube_quic_set", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetYoutubeQuic(a.B("on")) })
	reg("discord_status", func(ctx context.Context, a Args) (any, error) { return zapret.DiscordStatus(), nil })
	reg("discord_set", func(ctx context.Context, a Args) (any, error) {
		zapret.Mu.Lock()
		defer zapret.Mu.Unlock()
		return zapret.DiscordStatus(), zapret.SetDiscord(a.S("num"))
	})
	reg("discord_fake", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetDiscordFake(a.S("file")) })
	reg("game_status", func(ctx context.Context, a Args) (any, error) { return zapret.GameStatus(), nil })
	reg("game_set", func(ctx context.Context, a Args) (any, error) {
		zapret.Mu.Lock()
		defer zapret.Mu.Unlock()
		return nil, zapret.SetGame(a.S("choice"))
	})
	reg("game_fake", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetGameFake(a.S("file")) })
	reg("game_xtreme", func(ctx context.Context, a Args) (any, error) { return nil, zapret.ToggleXtreme() })
	reg("opt_set", func(ctx context.Context, a Args) (any, error) {
		if err := zapret.SetOpt(a.S("what"), a.S("val"), ctx); err != nil {
			return nil, err
		}
		return zapret.GetStatus(), nil
	})
	reg("nfqws_opt_get", func(ctx context.Context, a Args) (any, error) {
		return M{"content": zapret.GetOpt(), "args": argsPreview()}, nil
	})
	reg("nfqws_opt_set", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetRaw(a.S("content")) })
	reg("nochange_set", func(ctx context.Context, a Args) (any, error) { return nil, zapret.ListNochange(a.S("id"), a.B("on")) })
	reg("export_router", func(ctx context.Context, a Args) (any, error) { return M{"text": zapret.ExportRouter()}, nil })
	reg("ports_set", func(ctx context.Context, a Args) (any, error) {
		tcp, udp := strings.ReplaceAll(a.S("tcp"), " ", ""), strings.ReplaceAll(a.S("udp"), " ", "")
		for _, p := range strings.Split(tcp+","+udp, ",") {
			if p != "" && !portRx.MatchString(p) {
				return nil, fmt.Errorf("порт или диапазон указан неверно: %s", p)
			}
		}
		app.Update(func(s *app.Settings) { s.PortsTCP, s.PortsUDP = tcp, udp })
		return nil, zapret.Restart()
	})
	reg("lists_status", func(ctx context.Context, a Args) (any, error) {
		ls, err := zapret.ListsStatus()
		return M{"lists": ls}, err
	})
	reg("list_get", func(ctx context.Context, a Args) (any, error) {
		c, li, err := zapret.ListGet(a.S("id"))
		return M{"content": c, "info": li}, err
	})
	reg("list_set", func(ctx context.Context, a Args) (any, error) {
		li, err := zapret.ListSet(a.S("id"), a.S("content"))
		return M{"info": li}, err
	})
	reg("list_restore", func(ctx context.Context, a Args) (any, error) {
		li, err := zapret.ListRestore(ctx, a.S("id"))
		if err != nil {
			return nil, err
		}
		c, _, _ := zapret.ListGet(a.S("id"))
		return M{"content": c, "info": li}, nil
	})
	reg("excl_auto", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetExclAuto(a.S("v")) })
	reg("add_user_domains", func(ctx context.Context, a Args) (any, error) {
		ds, err := zapret.ParseDomains(a.S("domains"))
		if err != nil {
			return nil, err
		}
		if err := zapret.AddUserDomain(ds); err != nil {
			return nil, err
		}
		return nil, zapret.Restart()
	})

	// ---------- strategy test ----------
	reg("test_status", func(ctx context.Context, a Args) (any, error) { return TestStatus(), nil })
	reg("test_start", func(ctx context.Context, a Args) (any, error) {
		mode := a.S("mode")
		var doms []string
		if mode == "domain" {
			var err error
			if doms, err = zapret.ParseDomains(a.S("domains")); err != nil {
				return nil, err
			}
		}
		ok := false
		for _, m := range zapret.TestModes {
			if m == mode {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("неизвестный режим теста")
		}
		if mode != "current" && zapret.Guard() != nil {
			return nil, zapret.ErrNochange
		}
		if (mode == "custom" || mode == "custom_yt") && strings.TrimSpace(zapret.CustomTest()) == "" {
			return nil, fmt.Errorf("список своих стратегий пуст — добавьте их и сохраните")
		}
		return job("strategy_test", "Тест стратегий", func(j *app.Job) error {
			_, err := zapret.RunTest(j, mode, doms)
			return err
		})
	})
	reg("test_stop", func(ctx context.Context, a Args) (any, error) { return M{"ok": app.CancelJob("strategy_test")}, nil })
	reg("test_results", func(ctx context.Context, a Args) (any, error) { return zapret.LoadResults(a.S("mode")), nil })
	reg("test_clear", func(ctx context.Context, a Args) (any, error) { zapret.ClearResults(a.S("mode")); return nil, nil })
	reg("test_custom_set", func(ctx context.Context, a Args) (any, error) {
		n, err := zapret.SetCustomTest(a.S("content"))
		return M{"count": n}, err
	})
	reg("autobest_set", func(ctx context.Context, a Args) (any, error) {
		t, mode := a.S("time"), a.S("mode")
		if t != "" && t != "off" {
			var h, m int
			if n, _ := fmt.Sscanf(t, "%d:%d", &h, &m); n != 2 || h > 23 || m > 59 {
				return nil, fmt.Errorf("неверное время")
			}
			t = fmt.Sprintf("%02d:%02d", h, m)
		} else {
			t = ""
		}
		switch mode {
		case "v", "flowseal", "v_flowseal":
		default:
			mode = "v_flowseal"
		}
		app.Update(func(s *app.Settings) { s.AutobestTime, s.AutobestMode = t, mode })
		return TestStatus(), nil
	})
	reg("autobest_run", func(ctx context.Context, a Args) (any, error) {
		return job("strategy_test", "Автоподбор стратегии", zapret.AutoBest)
	})
	reg("autobest_clear", func(ctx context.Context, a Args) (any, error) { zapret.AutoLastClear(); return nil, nil })

	// ---------- Zapret2 ----------
	reg("z2_status", func(ctx context.Context, a Args) (any, error) { return zapret.GetStatus2(), nil })
	reg("z2_action", func(ctx context.Context, a Args) (any, error) {
		switch a.S("action") {
		case "install", "update":
			return job("install_zapret2", "Установка Zapret2", zapret.Install2)
		case "remove":
			return job("remove_zapret2", "Удаление Zapret2", zapret.Remove2)
		case "start":
			return nil, zapret.SetEnabled2(true)
		case "stop":
			return nil, zapret.SetEnabled2(false)
		}
		return nil, fmt.Errorf("неизвестное действие")
	})
	reg("z2_opt_get", func(ctx context.Context, a Args) (any, error) { return M{"content": zapret.Opt2Get()}, nil })
	reg("z2_opt_set", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetOpt2(a.S("content")) })
	reg("z2_ports_set", func(ctx context.Context, a Args) (any, error) { return nil, zapret.SetPorts2(a.S("tcp"), a.S("udp")) })

	// ---------- hosts ----------
	reg("hosts_status", func(ctx context.Context, a Args) (any, error) { return hosts.Status(), nil })
	reg("hosts_toggle", func(ctx context.Context, a Args) (any, error) {
		on, err := hosts.Toggle(a.S("id"))
		return M{"enabled": on}, err
	})
	reg("hosts_geohide", func(ctx context.Context, a Args) (any, error) { return nil, hosts.ReplaceGeoHide(ctx, a.S("region")) })
	reg("hosts_reset", func(ctx context.Context, a Args) (any, error) { return nil, hosts.Reset() })
	reg("hosts_file_get", func(ctx context.Context, a Args) (any, error) {
		t, err := hosts.FileGet()
		return M{"content": t}, err
	})
	reg("hosts_file_set", func(ctx context.Context, a Args) (any, error) { return nil, hosts.FileSet(a.S("content")) })

	// ---------- DoH ----------
	reg("doh_status", func(ctx context.Context, a Args) (any, error) {
		st := doh.Status(a.B("adapters"))
		st.HostsExtra = hosts.HasExtra()
		return st, nil
	})
	reg("doh_enable", func(ctx context.Context, a Args) (any, error) { return nil, doh.Enable(a.B("on")) })
	reg("doh_set", func(ctx context.Context, a Args) (any, error) {
		return nil, doh.SetProvider(a.S("provider"), a.S("custom"))
	})
	reg("doh_mode", func(ctx context.Context, a Args) (any, error) { return nil, doh.SetMode(a.S("mode")) })
	reg("doh_force", func(ctx context.Context, a Args) (any, error) { return nil, doh.SetForce(a.B("on")) })
	reg("doh_bootstrap", func(ctx context.Context, a Args) (any, error) { return nil, doh.SetBootstrap(a.S("value")) })
	reg("doh_test", func(ctx context.Context, a Args) (any, error) {
		ips, d, err := doh.Test(ctx, a.S("provider"), a.S("name"))
		if err != nil {
			return nil, err
		}
		return M{"ips": ips, "ms": d.Milliseconds()}, nil
	})

	// ---------- system ----------
	reg("system_status", func(ctx context.Context, a Args) (any, error) { return sysinfo.GetStatus(), nil })
	reg("connectivity", func(ctx context.Context, a Args) (any, error) { return sysinfo.Connectivity(), nil })
	reg("quic_toggle", func(ctx context.Context, a Args) (any, error) {
		on, err := sysinfo.ToggleQuic()
		return M{"quic_blocked": on}, err
	})
	reg("ipv6_toggle", func(ctx context.Context, a Args) (any, error) {
		on, err := sysinfo.ToggleIPv6()
		return M{"ipv6_enabled": on}, err
	})
	reg("expert_toggle", func(ctx context.Context, a Args) (any, error) { return M{"expert_mode": sysinfo.ToggleExpert()}, nil })
	reg("mirror_set", func(ctx context.Context, a Args) (any, error) { return nil, sysinfo.SetMirror(a.S("id")) })
	reg("time_sync", func(ctx context.Context, a Args) (any, error) {
		out, err := sysinfo.SyncTime()
		return M{"output": out}, err
	})
	reg("versions", func(ctx context.Context, a Args) (any, error) { return Versions(a.B("refresh")), nil })
	reg("uninstall_all", func(ctx context.Context, a Args) (any, error) {
		return job("uninstall_all", "Удаление Zapret Manager", UninstallAll)
	})

	// ---------- TG WS Proxy ----------
	reg("tg_status", func(ctx context.Context, a Args) (any, error) {
		return M{"items": tg.Status(), "lan_ip": sysinfo.LanIP()}, nil
	})
	reg("tg_install", func(ctx context.Context, a Args) (any, error) {
		id := a.S("id")
		return job("tg_install_"+id, "Установка TG WS Proxy", func(j *app.Job) error { return tg.Install(j, id) })
	})
	reg("tg_remove", func(ctx context.Context, a Args) (any, error) {
		id := a.S("id")
		return job("tg_remove_"+id, "Удаление TG WS Proxy", func(j *app.Job) error { return tg.Remove(j, id) })
	})
	reg("tg_action", func(ctx context.Context, a Args) (any, error) { return nil, tg.Action(a.S("id"), a.S("action")) })
	reg("tg_config", func(ctx context.Context, a Args) (any, error) {
		var c tg.Config
		if err := a.Into("config", &c); err != nil {
			return nil, err
		}
		return nil, tg.Configure(a.S("id"), c)
	})
	reg("tg_restart_all", func(ctx context.Context, a Args) (any, error) { tg.RestartAll(); return nil, nil })

	// ---------- AmneziaWG ----------
	reg("awg_status", func(ctx context.Context, a Args) (any, error) { return awg.Status(), nil })
	reg("awg_client", func(ctx context.Context, a Args) (any, error) {
		if a.S("action") == "remove" {
			return job("awg_client", "Удаление AmneziaWG", awg.RemoveClient)
		}
		return job("awg_client", "Установка AmneziaWG", awg.InstallClient)
	})
	reg("awg_conf_get", func(ctx context.Context, a Args) (any, error) {
		c, err := awg.GetConf(a.S("name"))
		return M{"content": c}, err
	})
	reg("awg_conf_set", func(ctx context.Context, a Args) (any, error) { return nil, awg.SaveConf(a.S("name"), a.S("content")) })
	reg("awg_tunnel", func(ctx context.Context, a Args) (any, error) {
		n := a.S("name")
		switch a.S("action") {
		case "start":
			return nil, awg.Start(n)
		case "stop":
			return nil, awg.Stop(n)
		case "delete":
			return nil, awg.Delete(n)
		case "full_on":
			return nil, awg.SetFullRoute(n, true)
		case "full_off":
			return nil, awg.SetFullRoute(n, false)
		}
		return nil, fmt.Errorf("неизвестное действие")
	})
	reg("warp_generate", func(ctx context.Context, a Args) (any, error) {
		var o awg.WarpOpts
		_ = a.Into("opts", &o)
		return job("warp", "Генерация WARP", func(j *app.Job) error { return awg.GenerateWarp(j, o) })
	})

	// ---------- ByeTube ----------
	reg("bt_status", func(ctx context.Context, a Args) (any, error) { return byetube.Status(), nil })
	reg("bt_action", func(ctx context.Context, a Args) (any, error) {
		switch a.S("action") {
		case "install", "update":
			return job("bytetube_install", "Установка ByeTube", byetube.Install)
		case "remove":
			return job("bytetube_remove", "Удаление ByeTube", byetube.Remove)
		case "start":
			return nil, byetube.SetEnabled(true)
		case "stop":
			return nil, byetube.SetEnabled(false)
		}
		return nil, fmt.Errorf("неизвестное действие")
	})
	reg("bt_config", func(ctx context.Context, a Args) (any, error) {
		var c byetube.Config
		if err := a.Into("config", &c); err != nil {
			return nil, err
		}
		return nil, byetube.Configure(c)
	})
	reg("bt_test", func(ctx context.Context, a Args) (any, error) {
		extra := zapret.Lines(a.S("extra"))
		return job("bytetube_test", "Тест стратегий ByeDPI", func(j *app.Job) error { return byetube.RunTest(j, extra) })
	})
	reg("bt_results", func(ctx context.Context, a Args) (any, error) { return M{"rows": byetube.Results()}, nil })

	// ---------- routing: Steer / Forkozz / Mixomo ----------
	reg("rt_status", func(ctx context.Context, a Args) (any, error) { return routing.Status(), nil })
	reg("rt_action", func(ctx context.Context, a Args) (any, error) {
		switch a.S("action") {
		case "install", "update":
			return job("routing", "Установка Mihomo", routing.Install)
		case "ui":
			return job("routing", "Установка веб-интерфейса", routing.InstallUI)
		case "remove":
			return job("routing", "Удаление Mihomo", routing.Remove)
		case "lists":
			return job("routing", "Обновление списков", func(j *app.Job) error {
				if err := routing.UpdateLists(j.Ctx(), j.Say, true); err != nil {
					j.Say("!! %v", err)
				}
				return routing.Restart()
			})
		case "start", "restart":
			return nil, routing.SetEngine(true)
		case "stop":
			return nil, routing.SetEngine(false)
		case "refresh_subs":
			return nil, routing.RefreshProviders()
		}
		return nil, fmt.Errorf("неизвестное действие")
	})
	reg("rt_section_save", func(ctx context.Context, a Args) (any, error) {
		var s routing.Section
		if err := a.Into("section", &s); err != nil {
			return nil, err
		}
		return job("routing", "Применение секции", func(j *app.Job) error {
			_, err := routing.SaveSection(j.Ctx(), s)
			if err == nil {
				j.Say("==> Секция «%s» сохранена и применена", s.Name)
			}
			return err
		})
	})
	reg("rt_section_delete", func(ctx context.Context, a Args) (any, error) { return nil, routing.DeleteSection(ctx, a.S("id")) })
	reg("rt_section_move", func(ctx context.Context, a Args) (any, error) {
		return nil, routing.MoveSection(ctx, a.S("id"), a.I("dir"))
	})
	reg("rt_globals", func(ctx context.Context, a Args) (any, error) {
		var g routing.Globals
		if err := a.Into("globals", &g); err != nil {
			return nil, err
		}
		return nil, routing.SetGlobals(ctx, g)
	})
	reg("rt_steer", func(ctx context.Context, a Args) (any, error) {
		var s routing.Steer
		if err := a.Into("steer", &s); err != nil {
			return nil, err
		}
		return job("routing", "Steer", func(j *app.Job) error {
			if err := routing.SaveSteer(j.Ctx(), s, j.Say); err != nil {
				return err
			}
			j.Say("==> Готово")
			return nil
		})
	})
	reg("rt_warp_recreate", func(ctx context.Context, a Args) (any, error) {
		return job("routing", "Новые ключи WARP", func(j *app.Job) error { return routing.RecreateWarp(j.Ctx(), j.Say) })
	})
	reg("rt_mixomo_get", func(ctx context.Context, a Args) (any, error) { return M{"content": routing.MixomoGet()}, nil })
	reg("rt_mixomo_set", func(ctx context.Context, a Args) (any, error) { return nil, routing.MixomoSet(a.S("content")) })
	reg("rt_mixomo_generate", func(ctx context.Context, a Args) (any, error) {
		var m routing.Mixomo
		if err := a.Into("mixomo", &m); err != nil {
			return nil, err
		}
		return job("routing", "Mixomo: конфигурация по подписке", func(j *app.Job) error {
			if err := routing.MixomoGenerate(j.Ctx(), m, j.Say); err != nil {
				return err
			}
			j.Say("==> Готово, Mixomo запущен")
			return nil
		})
	})
	reg("rt_mixomo_settings", func(ctx context.Context, a Args) (any, error) {
		var m routing.Mixomo
		if err := a.Into("mixomo", &m); err != nil {
			return nil, err
		}
		return nil, routing.MixomoSettings(ctx, m)
	})
	reg("rt_groups", func(ctx context.Context, a Args) (any, error) {
		g, err := routing.Groups()
		return M{"groups": g}, err
	})
	reg("rt_select", func(ctx context.Context, a Args) (any, error) { return nil, routing.Select(a.S("group"), a.S("node")) })
	reg("rt_delay", func(ctx context.Context, a Args) (any, error) {
		d, err := routing.TestDelay(a.S("group"))
		return M{"delays": d}, err
	})
	reg("rt_explain", func(ctx context.Context, a Args) (any, error) { return routing.ExplainRoute(a.S("target")), nil })
}

func argsPreview() string {
	a, err := zapret.BuildArgs()
	if err != nil {
		return "(" + err.Error() + ")"
	}
	return "winws.exe " + strings.Join(a, " ")
}

// ---------- aggregate status for the dashboard ----------

type DashInfo struct {
	Sys      sysinfo.Info       `json:"sys"`
	Zapret   zapret.Status      `json:"zapret"`
	Zapret2  zapret.Status2     `json:"zapret2"`
	DoH      doh.StatusInfo     `json:"doh"`
	Hosts    int                `json:"hosts_blocks"`
	TG       []tg.ItemStatus    `json:"tg"`
	AWG      []awg.Tunnel       `json:"awg"`
	Bt       byetube.StatusInfo `json:"bytetube"`
	Routing  routing.StatusInfo `json:"routing"`
	Internet bool               `json:"internet"`
	Jobs     []app.JobStatus    `json:"jobs"`
	Theme    string             `json:"theme"`
	Quic     bool               `json:"quic_blocked"`
}

var (
	inetMu sync.Mutex
	inetOK bool
	inetAt time.Time
)

func Dashboard() DashInfo {
	inetMu.Lock()
	if time.Since(inetAt) > 30*time.Second {
		inetAt = time.Now()
		go func() {
			ok := sysinfo.InternetOK()
			inetMu.Lock()
			inetOK = ok
			inetMu.Unlock()
		}()
	}
	ok := inetOK
	inetMu.Unlock()
	d := DashInfo{Sys: sysinfo.Get(), Zapret: zapret.GetStatus(), Zapret2: zapret.GetStatus2(), DoH: doh.Status(false),
		TG: tg.Status(), AWG: awg.Tunnels(), Bt: byetube.Status(), Routing: routing.Status(), Internet: ok,
		Jobs: app.RunningJobs(), Theme: app.S().Theme, Quic: sysinfo.GetStatus().QuicBlocked}
	for _, it := range hosts.Status().Items {
		if it.Enabled {
			d.Hosts++
		}
	}
	d.Routing.Log = nil
	return d
}

func TestStatus() M {
	j := app.JobState("strategy_test")
	has := M{}
	for _, m := range zapret.TestModes {
		has[m] = zapret.LoadResults(m) != nil
	}
	s := app.S()
	return M{"running": j.Running, "job": j, "has": has, "custom": zapret.CustomTest(),
		"auto": M{"time": s.AutobestTime, "mode": s.AutobestMode, "last": zapret.AutoLastGet()}}
}
