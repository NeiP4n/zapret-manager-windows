package web

import (
	"context"
	"os"
	"os/exec"
	"regexp"
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
	"github.com/zapretmanager/zmwin/internal/tg"
	"github.com/zapretmanager/zmwin/internal/zapret"
)

var portRx = regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{1,5})?$`)

// DefaultHostsBlocks — what the router's full install adds to hosts.
var DefaultHostsBlocks = strings.Fields("ai instagram ntc librusec telegram twitch scell spotify rutor")

// FullReinstall: remove → install → v7 → hosts presets → game Gv1 (TUI menu item "f").
func FullReinstall(j *app.Job) error {
	if zapret.Installed() {
		if err := zapret.Remove(j); err != nil {
			return err
		}
	}
	if err := zapret.Install(j); err != nil {
		return err
	}
	zapret.Mu.Lock()
	defer zapret.Mu.Unlock()
	if zapret.Guard() == nil {
		j.Say("==> Применяем базовую стратегию v7")
		if err := zapret.SetV(7); err != nil {
			j.Say("!! %v", err)
		}
	}
	j.Say("==> Добавляем домены в hosts")
	if err := hosts.AddBlocks(DefaultHostsBlocks); err != nil {
		j.Say("!! %v", err)
	}
	if zapret.Guard() == nil {
		j.Say("==> Настраиваем игровую стратегию Gv1")
		if err := zapret.SetGame("Gv1"); err != nil {
			j.Say("!! %v", err)
		}
	}
	j.Say("==> Готово, Zapret установлен и настроен")
	return nil
}

type VerItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Newer     bool   `json:"newer"`
}

var (
	verMu    sync.Mutex
	verCache []VerItem
	verAt    time.Time
)

// Versions compares installed components with their latest GitHub releases (router: versions).
func Versions(refresh bool) M {
	verMu.Lock()
	defer verMu.Unlock()
	if refresh || time.Since(verAt) > 6*time.Hour || verCache == nil {
		s := app.S()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		type src struct{ id, name, inst, repo string }
		bt := byetube.Status()
		rt := routing.Status()
		list := []src{
			{"zapret", "Zapret (winws)", s.ZapretVersion, "bol-van/zapret"},
			{"zapret2", "Zapret2 (winws2)", s.Zapret2Version, "bol-van/zapret2"},
			{"mihomo", "Mihomo", rt.Config.Version, "MetaCubeX/mihomo"},
			{"byedpi", "ByeDPI", bt.Config.Version, "hufrea/byedpi"},
		}
		for _, t := range tg.Status() {
			list = append(list, src{"tg-" + t.ID, t.Title, t.Config.Version, map[string]string{"go": "d0mhate/-tg-ws-proxy-Manager-go", "rs": "valnesfjord/tg-ws-proxy-rs"}[t.ID]})
		}
		var out []VerItem
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, x := range list {
			wg.Add(1)
			go func(x src) {
				defer wg.Done()
				lt := app.LatestTag(ctx, x.repo)
				it := VerItem{ID: x.id, Name: x.name, Installed: strings.TrimPrefix(x.inst, "v"), Latest: strings.TrimPrefix(lt, "v")}
				it.Newer = it.Installed != "" && it.Latest != "" && it.Installed != it.Latest
				mu.Lock()
				out = append(out, it)
				mu.Unlock()
			}(x)
		}
		wg.Wait()
		order := map[string]int{}
		for i, x := range list {
			order[x.name] = i
		}
		for i := 1; i < len(out); i++ {
			for k := i; k > 0 && order[out[k].Name] < order[out[k-1].Name]; k-- {
				out[k], out[k-1] = out[k-1], out[k]
			}
		}
		si := SelfCheck(true)
		out = append([]VerItem{{ID: "self", Name: "Zapret Manager", Installed: app.Version, Latest: si.Latest, Newer: si.Newer}}, out...)
		verCache, verAt = out, time.Now()
	}
	return M{"items": verCache, "ts": verAt.Unix()}
}

// UninstallAll removes everything and restores the system (router: system_uninstall_panel +
// removal of every component).
func UninstallAll(j *app.Job) error {
	j.Say("==> Останавливаем маршрутизацию и прокси")
	routing.Stop()
	byetube.Shutdown()
	tg.StopAll()
	for _, t := range awg.Tunnels() {
		_ = awg.Stop(t.Name)
	}
	j.Say("==> Возвращаем DNS адаптеров")
	_ = doh.Enable(false)
	j.Say("==> Останавливаем Zapret и выгружаем WinDivert")
	osx.StopAll()
	osx.UnloadWinDivert()
	j.Say("==> Убираем правила брандмауэра")
	_, _ = osx.PS(nil, 60*time.Second, `Get-NetFirewallRule -DisplayName 'ZM *' -EA SilentlyContinue | Remove-NetFirewallRule`)
	j.Say("==> Удаляем ярлык и службу")
	RemoveShortcut()
	if !app.Dev {
		// delete files after the service process exits
		cmd := exec.Command("cmd.exe", "/c", "ping -n 6 127.0.0.1 >nul & sc.exe delete "+app.ServiceName+" & rmdir /s /q \""+app.Base+"\"")
		osx.Detach(cmd)
		_ = cmd.Start()
		go func() {
			time.Sleep(2 * time.Second)
			_ = osx.ServiceStop()
			os.Exit(0)
		}()
	}
	j.Say("==> Готово. Zapret Manager удалён, система возвращена к исходному состоянию")
	return nil
}

// RemoveShortcut deletes the desktop shortcut created by the installer.
func RemoveShortcut() {
	_, _ = osx.PS(nil, 20*time.Second, `$p=[Environment]::GetFolderPath('CommonDesktopDirectory'); Remove-Item (Join-Path $p 'Zapret Manager.lnk') -EA SilentlyContinue`)
}
