package zapret

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

// Zapret2 = winws2 with lua desync programs (bol-van/zapret2). Like on the router it is an
// alternative to Zapret, not an add-on: both grab the same traffic through WinDivert.

const (
	Proc2Name   = "winws2"
	Default2Tag = "v1.0.5.2"
)

var default2Opt = []string{
	"--filter-tcp=80 --filter-l7=http <HOSTLIST> --payload=http_req --lua-desync=fake:blob=fake_default_http:tcp_md5 --lua-desync=multisplit:pos=method+2 --new",
	"--filter-tcp=443 --filter-l7=tls <HOSTLIST> --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000 --lua-desync=multidisorder:pos=1,midsld --new",
	"--filter-udp=443 --filter-l7=quic <HOSTLIST_NOAUTO> --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=6",
}

func Winws2Exe() string  { return filepath.Join(app.BinZapret2, "winws2.exe") }
func Installed2() bool   { return app.Exists(Winws2Exe()) || (app.Dev && app.Exists(body2Path())) }
func body2Path() string  { return app.P("state", "strategy2.txt") }
func Opt2Get() string    { return app.ReadText(body2Path()) }
func ports2Path() string { return app.P("state", "zapret2_ports.json") }

type Ports2 struct {
	TCP string `json:"tcp"`
	UDP string `json:"udp"`
}

var ports2KV = app.NewKV("zapret2_ports")

func GetPorts2() Ports2 {
	p := Ports2{TCP: "80,443", UDP: "443"}
	ports2KV.Load(&p)
	return p
}

type Status2 struct {
	Installed bool          `json:"installed"`
	Running   bool          `json:"running"`
	Enabled   bool          `json:"enabled"`
	Version   string        `json:"version"`
	Zapret    bool          `json:"zapret_installed"`
	Expert    bool          `json:"expert"`
	Ports     Ports2        `json:"ports"`
	Proc      osx.ProcState `json:"proc"`
}

func GetStatus2() Status2 {
	s := app.S()
	st := Status2{Installed: Installed2(), Enabled: s.Zapret2Enabled, Version: s.Zapret2Version,
		Zapret: Installed(), Expert: s.Expert, Ports: GetPorts2(), Proc: osx.ProcStatus(Proc2Name)}
	st.Running = st.Proc.Running
	return st
}

func build2Args() ([]string, error) {
	p := GetPorts2()
	lua := filepath.Join(app.BinZapret2, "lua")
	args := []string{}
	if p.TCP != "" {
		args = append(args, "--wf-tcp="+p.TCP)
	}
	if p.UDP != "" {
		args = append(args, "--wf-udp="+p.UDP)
	}
	if app.S().DisableIPv6 {
		args = append(args, "--wf-l3=ipv4")
	}
	for _, f := range []string{"zapret-lib.lua", "zapret-antidpi.lua", "zapret-auto.lua"} {
		args = append(args, "--lua-init=@"+filepath.Join(lua, f))
	}
	n := 0
	for _, l := range Lines(Opt2Get()) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		t = strings.ReplaceAll(t, "<HOSTLIST_NOAUTO>", "--hostlist-exclude="+ExcludeList)
		t = strings.ReplaceAll(t, "<HOSTLIST>", "--hostlist-exclude="+ExcludeList)
		for _, f := range strings.Fields(t) {
			args = append(args, absPathArg(strings.ReplaceAll(f, `"`, "")))
			n++
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("стратегия Zapret2 пуста")
	}
	return args, nil
}

func Restart2() error {
	s := app.S()
	if !Installed2() || !s.Zapret2Enabled {
		osx.ProcStop(Proc2Name)
		return nil
	}
	args, err := build2Args()
	if err != nil {
		return err
	}
	if err := osx.ProcStart(osx.ProcSpec{Name: Proc2Name, Exe: Winws2Exe(), Args: args, Dir: app.BinZapret2, Restart: true}); err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)
	if st := osx.ProcStatus(Proc2Name); !st.Running && !app.Dev {
		return fmt.Errorf("winws2 завершился сразу после запуска: %s", strings.Join(osx.TailLog(Proc2Name, 4), " / "))
	}
	return nil
}

// SetEnabled2 starts/stops Zapret2. Zapret and Zapret2 can't run together unless expert mode.
func SetEnabled2(on bool) error {
	if !Installed2() {
		return fmt.Errorf("Zapret2 не установлен")
	}
	if on && app.S().ZapretEnabled && osx.ProcRunning(ProcName) && !app.S().Expert {
		return fmt.Errorf("работает Zapret — он несовместим с Zapret2. Остановите Zapret или включите Expert mode")
	}
	app.Update(func(s *app.Settings) { s.Zapret2Enabled = on })
	return Restart2()
}

func SetOpt2(content string) error {
	content = NormalizePaths(strings.ReplaceAll(content, "\r", ""))
	if !strings.Contains(content, "--") {
		return fmt.Errorf("в стратегии нет ни одного параметра --…")
	}
	if err := app.WriteText(body2Path(), strings.TrimSpace(content)+"\n"); err != nil {
		return err
	}
	return Restart2()
}

func SetPorts2(tcp, udp string) error {
	if err := ports2KV.Save(&Ports2{TCP: strings.TrimSpace(tcp), UDP: strings.TrimSpace(udp)}); err != nil {
		return err
	}
	return Restart2()
}

func Install2(j *app.Job) error {
	ctx := j.Ctx()
	if Installed() && !app.S().Expert {
		return fmt.Errorf("установлен Zapret — он несовместим с Zapret2. Сначала удалите Zapret (или включите Expert mode)")
	}
	j.Say("==> Определяем версию Zapret2")
	tag := app.LatestTag(ctx, "bol-van/zapret2")
	if tag == "" {
		tag = Default2Tag
		j.Say("!! Не удалось определить версию, использую %s", tag)
	}
	url := fmt.Sprintf("%s/bol-van/zapret2/releases/download/%s/zapret2-%s.zip", app.GHMain, tag, tag)
	zp := filepath.Join(app.TmpDir, "zapret2.zip")
	j.Say("==> Скачиваем %s", url)
	if err := app.Download(ctx, url, zp, 5, j); err != nil {
		return fmt.Errorf("не удалось скачать архив Zapret2")
	}
	defer os.Remove(zp)
	osx.ProcStop(Proc2Name)
	j.Say("==> Распаковываем winws2, lua-скрипты и fake-файлы")
	n, err := app.Unzip(zp, app.Base, func(name string) string {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		rel := parts[1]
		switch {
		case strings.HasPrefix(rel, "binaries/windows-x86_64/"):
			return "bin/zapret2/" + strings.TrimPrefix(rel, "binaries/windows-x86_64/")
		case strings.HasPrefix(rel, "lua/"):
			return "bin/zapret2/" + rel
		case strings.HasPrefix(rel, "files/fake/"):
			if app.Exists(filepath.Join(app.Base, filepath.FromSlash(rel))) {
				return ""
			}
			return rel
		}
		return ""
	})
	if err != nil {
		return fmt.Errorf("ошибка распаковки: %v", err)
	}
	j.Say("   ✓ Файлов: %d", n)
	_ = os.MkdirAll(app.ListsDir, 0o755)
	if !app.Exists(ListPath(ExcludeList)) {
		_ = RefreshExclude(ctx)
	}
	if strings.TrimSpace(Opt2Get()) == "" {
		_ = app.WriteText(body2Path(), strings.Join(default2Opt, "\n")+"\n")
	}
	app.Update(func(s *app.Settings) { s.Zapret2Version = strings.TrimPrefix(tag, "v"); s.Zapret2Enabled = true })
	j.Say("==> Запускаем winws2")
	if err := Restart2(); err != nil {
		return err
	}
	j.Say("==> Готово, Zapret2 установлен (%s)", tag)
	return nil
}

func Remove2(j *app.Job) error {
	j.Say("==> Останавливаем Zapret2")
	osx.ProcStop(Proc2Name)
	osx.UnloadWinDivert()
	app.Remove(app.BinZapret2, body2Path(), ports2Path())
	_ = os.MkdirAll(app.BinZapret2, 0o755)
	app.Update(func(s *app.Settings) { s.Zapret2Enabled, s.Zapret2Version = false, "" })
	j.Say("==> Готово, Zapret2 удалён")
	return nil
}
