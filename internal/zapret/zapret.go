// Package zapret manages winws.exe (the Windows build of nfqws) and its strategy — the Windows
// counterpart of the router's /etc/init.d/zapret + NFQWS_OPT handling in backend.sh.
package zapret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

const (
	ProcName      = "winws"
	DefaultTag    = "v72.13"
	FlowsealZip   = "https://github.com/Flowseal/zapret-discord-youtube/archive/refs/heads/main.zip"
	FlowsealRaw   = "https://github.com/Flowseal/zapret-discord-youtube/raw/refs/heads/main/bin"
	ExcludeURL    = "https://raw.githubusercontent.com/StressOzz/Zapret-Manager/refs/heads/main/zapret-hosts-user-exclude.txt"
	YoutubeStrURL = "https://raw.githubusercontent.com/StressOzz/Zapret-Manager/refs/heads/main/files/StrYoutube"
	RknURL        = "https://raw.githubusercontent.com/IndeecFOX/zapret4rocket/refs/heads/master/extra_strats/TCP/RKN/List.txt"
)

// Mu serialises every change of the strategy / ports (the router relies on one rpcd call at a time).
var Mu sync.Mutex

var (
	ErrNotInstalled = errors.New("Zapret не установлен")
	ErrNochange     = errors.New("включено «Не изменять стратегию» (#nochange) — панель её не меняет. Выключите этот переключатель в «Редактировать текущую стратегию», чтобы разрешить изменения")
	ErrLayout       = errors.New("блок стратегий записан не так, как его пишет панель, — изменение не применено")
)

func WinwsExe() string     { return filepath.Join(app.BinZapret, "winws.exe") }
func Installed() bool      { return app.Exists(WinwsExe()) || (app.Dev && app.Exists(bodyPath())) }
func bodyPath() string     { return app.P("state", "strategy.txt") }
func flowsealFile() string { return app.P("state", "flowseal_strategies.txt") }
func youtubeFile() string  { return app.P("state", "youtube_strategies.txt") }
func ListPath(rel string) string {
	return filepath.Join(app.Base, filepath.FromSlash(rel))
}

// LoadBody returns the current strategy lines.
func LoadBody() []string { return Lines(app.ReadText(bodyPath())) }

func SaveBody(body []string) error { return app.WriteText(bodyPath(), Join(body)+"\n") }

// Guard refuses changes when the strategy carries #nochange.
func Guard() error {
	if Nochange(app.ReadText(bodyPath())) {
		return ErrNochange
	}
	return nil
}

// ---------------- ports (router: option NFQWS_PORTS_TCP / NFQWS_PORTS_UDP) ----------------

func splitPorts(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func portsAdd(cur, add string) string {
	have := map[string]bool{}
	list := splitPorts(cur)
	for _, p := range list {
		have[p] = true
	}
	for _, p := range splitPorts(add) {
		if !have[p] {
			list = append(list, p)
			have[p] = true
		}
	}
	return strings.Join(list, ",")
}

func portsRemove(cur, rm string) string {
	drop := map[string]bool{}
	for _, p := range splitPorts(rm) {
		drop[p] = true
	}
	var out []string
	for _, p := range splitPorts(cur) {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

func AddPorts(tcp, udp string) {
	app.Update(func(s *app.Settings) {
		if tcp != "" {
			s.PortsTCP = portsAdd(s.PortsTCP, tcp)
		}
		if udp != "" {
			s.PortsUDP = portsAdd(s.PortsUDP, udp)
		}
	})
}

func RemovePorts(tcp, udp string) {
	app.Update(func(s *app.Settings) {
		if tcp != "" {
			s.PortsTCP = portsRemove(s.PortsTCP, tcp)
		}
		if udp != "" {
			s.PortsUDP = portsRemove(s.PortsUDP, udp)
		}
	})
}

// ---------------- winws process ----------------

var placeholderRx = regexp.MustCompile(`<HOSTLIST(_NOAUTO)?>`)

// BuildArgs turns settings + strategy into the winws command line.
func BuildArgs() ([]string, error) {
	s := app.S()
	var args []string
	if s.PortsTCP != "" {
		args = append(args, "--wf-tcp="+s.PortsTCP)
	}
	if s.PortsUDP != "" {
		args = append(args, "--wf-udp="+s.PortsUDP)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("не заданы порты TCP/UDP для перехвата")
	}
	if s.DisableIPv6 {
		args = append(args, "--wf-l3=ipv4")
	}
	n := 0
	for _, l := range LoadBody() {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		t = placeholderRx.ReplaceAllString(t, "--hostlist="+UserList+" --hostlist-exclude="+ExcludeList)
		for _, f := range strings.Fields(t) {
			f = strings.ReplaceAll(f, `"`, "")
			args = append(args, absPathArg(f))
			n++
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("стратегия пуста — выберите стратегию")
	}
	return args, nil
}

func absPathArg(a string) string {
	i := strings.Index(a, "=")
	if i < 0 {
		return a
	}
	v := a[i+1:]
	at := ""
	if strings.HasPrefix(v, "@") {
		at, v = "@", v[1:]
	}
	if strings.HasPrefix(v, FK) || strings.HasPrefix(v, LS) {
		return a[:i+1] + at + ListPath(v)
	}
	return a
}

// Restart applies the configuration: (re)start winws if enabled, otherwise stop it.
func Restart() error {
	if !Installed() {
		osx.ProcStop(ProcName)
		return nil
	}
	if !app.S().ZapretEnabled {
		osx.ProcStop(ProcName)
		return nil
	}
	args, err := BuildArgs()
	if err != nil {
		osx.ProcStop(ProcName)
		return err
	}
	err = osx.ProcStart(osx.ProcSpec{Name: ProcName, Exe: WinwsExe(), Args: args, Dir: app.BinZapret, Restart: true})
	if err == nil {
		time.Sleep(700 * time.Millisecond)
		if st := osx.ProcStatus(ProcName); !st.Running && !app.Dev {
			return fmt.Errorf("winws завершился сразу после запуска: %s", strings.Join(osx.TailLog(ProcName, 4), " / "))
		}
	}
	return err
}

func SetEnabled(on bool) error {
	if !Installed() {
		return ErrNotInstalled
	}
	app.Update(func(s *app.Settings) { s.ZapretEnabled = on })
	return Restart()
}

// ---------------- status ----------------

type Status struct {
	Installed bool          `json:"installed"`
	Running   bool          `json:"running"`
	Enabled   bool          `json:"enabled"`
	Version   string        `json:"version"`
	Strategy  string        `json:"strategy"`
	Flowseal  string        `json:"flowseal"`
	YvOff     bool          `json:"yv_off"`
	DvOff     bool          `json:"dv_off"`
	QuicYT    bool          `json:"quic_yt"`
	Expert    bool          `json:"expert"`
	Rkn       bool          `json:"rkn"`
	RknOn     bool          `json:"rkn_on"`
	RknFits   bool          `json:"rkn_fits"`
	RknCount  int           `json:"rkn_count"`
	Wssize    bool          `json:"wssize"`
	Nochange  bool          `json:"nochange"`
	PortsTCP  string        `json:"ports_tcp"`
	PortsUDP  string        `json:"ports_udp"`
	IPv6      bool          `json:"ipv6"`
	TsWarning bool          `json:"ts_warning"`
	Proc      osx.ProcState `json:"proc"`
	Conflicts []string      `json:"conflicts"`
	Zapret2   bool          `json:"zapret2_installed"`
}

func GetStatus() Status {
	s := app.S()
	body := LoadBody()
	text := Join(body)
	st := Status{
		Installed: Installed(), Enabled: s.ZapretEnabled, Version: s.ZapretVersion,
		Strategy: Name(body), Flowseal: s.FlowsealName, YvOff: s.YvOff, DvOff: s.DvOff,
		QuicYT: hasLine(body, "--filter-udp=443"), Expert: s.Expert, Rkn: s.RknOn,
		RknOn: rknOn(body), Wssize: hasLine(body, wssLine), Nochange: Nochange(text),
		PortsTCP: s.PortsTCP, PortsUDP: s.PortsUDP, IPv6: !s.DisableIPv6,
		TsWarning: strings.Contains(text, "=ts"), Proc: osx.ProcStatus(ProcName),
	}
	st.Running = st.Proc.Running
	st.RknFits = st.RknOn || hasLine(body, exclHL)
	st.RknCount = countLines(ListPath(RknList))
	st.Conflicts = Conflicts()
	st.Zapret2 = app.Exists(filepath.Join(app.BinZapret2, "winws2.exe"))
	return st
}

// Conflicts lists other DPI tools that grab WinDivert and break winws (Windows analogue of the
// TUI warning about byedpi / youtubeUnblock on the router).
func Conflicts() []string {
	var out []string
	ps := osx.Processes()
	ours := osx.ProcStatus(ProcName).PID
	for _, pid := range ps["winws.exe"] {
		if pid != ours {
			out = append(out, "запущен сторонний winws.exe (Flowseal / zapret-win-bundle) — остановите его")
			break
		}
	}
	if len(ps["goodbyedpi.exe"]) > 0 {
		out = append(out, "запущен GoodbyeDPI — он конфликтует с winws")
	}
	for _, svc := range []string{"zapret", "GoodbyeDPI"} {
		if osx.ServiceState(svc) == "running" {
			out = append(out, "работает служба «"+svc+"» — удалите её (service.bat → Remove Services)")
		}
	}
	return out
}

func countLines(p string) int {
	n := 0
	for _, l := range strings.Split(app.ReadText(p), "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "#") {
			n++
		}
	}
	return n
}

// ---------------- install / remove ----------------

// Install downloads the latest bol-van/zapret release and installs the Windows binaries
// (router: _do_install_zapret_core with remittor/zapret-openwrt packages).
func Install(j *app.Job) error {
	ctx := j.Ctx()
	j.Say("==> Определяем версию Zapret")
	tag := app.LatestTag(ctx, "bol-van/zapret")
	if tag == "" {
		j.Say("!! Не удалось определить версию, использую %s", DefaultTag)
		tag = DefaultTag
	}
	url := fmt.Sprintf("%s/bol-van/zapret/releases/download/%s/zapret-%s.zip", app.GHMain, tag, tag)
	zip := filepath.Join(app.TmpDir, "zapret.zip")
	j.Say("==> Скачиваем %s", url)
	if err := app.Download(ctx, url, zip, 5, j); err != nil {
		return fmt.Errorf("не удалось скачать архив — проверьте соединение с GitHub или смените зеркало в «Системе»")
	}
	defer os.Remove(zip)

	if Installed() {
		j.Say("==> Останавливаем текущий Zapret")
	}
	osx.ProcStop(ProcName)
	osx.UnloadWinDivert()

	j.Say("==> Распаковываем winws и fake-файлы")
	n, err := app.Unzip(zip, app.Base, func(name string) string {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			return ""
		}
		rel := parts[1]
		switch {
		case strings.HasPrefix(rel, "binaries/windows-x86_64/"):
			return "bin/zapret/" + strings.TrimPrefix(rel, "binaries/windows-x86_64/")
		case strings.HasPrefix(rel, "files/fake/"):
			return rel
		}
		return ""
	})
	if err != nil {
		return fmt.Errorf("ошибка распаковки: %v", err)
	}
	if !app.Exists(WinwsExe()) && !app.Dev {
		return fmt.Errorf("в архиве нет winws.exe (распаковано файлов: %d)", n)
	}
	j.Say("   ✓ Файлов: %d", n)
	AddFlowFakes(ctx, j)

	j.Say("==> Готовим списки доменов")
	ensureLists(ctx, j)

	ver := strings.TrimPrefix(tag, "v")
	app.Update(func(s *app.Settings) { s.ZapretVersion = ver; s.ZapretEnabled = true })
	if len(LoadBody()) == 0 {
		j.Say("==> Стратегии ещё нет — применяем базовую v7")
		if err := SetV(7); err != nil {
			j.Say("!! %v", err)
		}
	}
	j.Say("==> Запускаем winws")
	if err := Restart(); err != nil {
		return err
	}
	j.Say("==> Готово, Zapret установлен (%s)", ver)
	return nil
}

// AddFlowFakes fetches the extra fake payloads Flowseal strategies reference (do_add_fake_flow),
// plus the aliases the router uses (4pda.bin, tls_clienthello_www_onetrust_com.bin).
func AddFlowFakes(ctx context.Context, j *app.Job) {
	type item struct{ src, dst string }
	var items []item
	for _, f := range flowFakes {
		items = append(items, item{f, f})
	}
	items = append(items, item{"tls_clienthello_4pda_to.bin", "4pda.bin"}, item{"tls_clienthello_max_ru.bin", "tls_clienthello_www_onetrust_com.bin"},
		item{"tls_clienthello_max_ru.bin", "tls_clienthello_max_ru.bin"}, item{"tls_clienthello_4pda_to.bin", "tls_clienthello_4pda_to.bin"})
	said := false
	for _, it := range items {
		dst := filepath.Join(app.FakeDir, it.dst)
		if app.Exists(dst) {
			continue
		}
		if !said && j != nil {
			j.Say("==> Скачиваем дополнительные fake-файлы")
			said = true
		}
		if err := app.Download(ctx, FlowsealRaw+"/"+it.src, dst, 2, nil); err != nil && j != nil {
			j.Say("!! Не удалось загрузить файл %s", it.src)
		}
	}
}

func ensureLists(ctx context.Context, j *app.Job) {
	_ = os.MkdirAll(app.ListsDir, 0o755)
	if !app.Exists(ListPath(UserList)) {
		_ = app.WriteText(ListPath(UserList), "")
	}
	if err := RefreshExclude(ctx); err != nil && j != nil {
		j.Say("!! Список исключений не скачался — оставлен прежний")
	}
	if !app.Exists(ListPath(ExcludeList)) {
		_ = app.WriteText(ListPath(ExcludeList), "")
	}
	AddGPDomains()
}

// Remove uninstalls Zapret completely (do_remove_zapret).
func Remove(j *app.Job) error {
	j.Say("==> Останавливаем Zapret")
	osx.ProcStop(ProcName)
	osx.UnloadWinDivert()
	j.Say("   ✓ winws остановлен, драйвер WinDivert выгружен")
	j.Say("==> Удаляем файлы, стратегии и списки")
	app.Remove(app.BinZapret, app.FakeDir, app.ListsDir, bodyPath(), flowsealFile(), youtubeFile(), TestDir())
	app.Update(func(s *app.Settings) {
		s.ZapretEnabled, s.ZapretVersion, s.FlowsealName = false, "", ""
		s.YvOff, s.DvOff, s.RknOn, s.XtremeBackup = false, false, false, nil
		s.PortsTCP, s.PortsUDP = "80,443", "443"
		s.AutobestTime, s.ExclAuto = "", "off"
	})
	_ = os.MkdirAll(app.BinZapret, 0o755)
	_ = os.MkdirAll(app.FakeDir, 0o755)
	_ = os.MkdirAll(app.ListsDir, 0o755)
	j.Say("==> Готово, Zapret удалён полностью")
	return nil
}

// ---------------- lists helpers ----------------

// AddGPDomains merges Google Play / YouTube hosts into the google hostlist (_add_gp_domains).
func AddGPDomains() {
	p := ListPath(GoogleList)
	cur := app.ReadText(p)
	if Nochange(cur) {
		return
	}
	set := map[string]bool{}
	var out []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d != "" && !set[d] {
			set[d] = true
			out = append(out, d)
		}
	}
	if strings.TrimSpace(cur) == "" {
		for _, d := range googleDefaults {
			add(d)
		}
	}
	for _, l := range strings.Split(cur, "\n") {
		add(l)
	}
	for _, d := range gpDomains {
		add(d)
	}
	sortStrings(out)
	_ = app.WriteText(p, strings.Join(out, "\n")+"\n")
}

// RefreshExclude re-downloads the exclusion list unless it is protected with #nochange.
func RefreshExclude(ctx context.Context) error {
	p := ListPath(ExcludeList)
	if Nochange(app.ReadText(p)) {
		return nil
	}
	t, err := app.FetchText(ctx, ExcludeURL, 25*time.Second)
	if err != nil || strings.TrimSpace(t) == "" {
		return fmt.Errorf("не скачался")
	}
	return app.WriteText(p, t)
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for k := i; k > 0 && a[k] < a[k-1]; k-- {
			a[k], a[k-1] = a[k-1], a[k]
		}
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
