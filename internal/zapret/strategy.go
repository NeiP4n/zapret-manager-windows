package zapret

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
)

var (
	yvMarkRx   = regexp.MustCompile(`^#[ \t]*(Yv[0-9]+)$`)
	dvMarkRx   = regexp.MustCompile(`^#[ \t]*Dv([0-9]+)`)
	gvMarkRx   = regexp.MustCompile(`^#Gv([1-4])(Xtreme)?$`)
	gvXtRx     = regexp.MustCompile(`^#Gv[0-9]+Xtreme$`)
	gvPlainRx  = regexp.MustCompile(`^#Gv[0-9]+$`)
	discordRx  = regexp.MustCompile(`^--filter-l7=discord,stun$|^--filter-udp=19294-19344,50000-50100$|^--filter-tcp=2053,2083,2087,2096,8443$|^--hostlist-domains=discord[.]media$|^#[ \t]*Dv[0-9]+$`)
	yvRx       = regexp.MustCompile(`^#[ \t]*Yv[0-9]+$|^--hostlist=` + regexp.QuoteMeta(GoogleList) + `$`)
	udp443Rx   = regexp.MustCompile(`^--filter-udp=443$`)
	wssRx      = regexp.MustCompile(`^--wssize 1:6$`)
	dvTCPLine  = regexp.MustCompile(`^[ \t]*--filter-tcp=2053,2083,2087,2096,8443`)
	gameRx     = regexp.MustCompile(`^#Gv[0-9]+(Xtreme)?$|^--filter-(udp|tcp)=(` + regexp.QuoteMeta(PortsGameUDP) + `|` + regexp.QuoteMeta(PortsGameTCP) + `|80,88,444-65535)$`)
	fakeDiscRx = regexp.MustCompile(`^--dpi-desync-fake-discord=`)
)

const wssLine = "--wssize 1:6"

// mutate loads the body, applies fn, saves and restarts winws. fn returns restart=false to skip.
func mutate(fn func(body []string) ([]string, error)) error {
	if !Installed() {
		return ErrNotInstalled
	}
	body, err := fn(LoadBody())
	if err != nil {
		return err
	}
	if err := SaveBody(body); err != nil {
		return err
	}
	return Restart()
}

// ---------------- QUIC for YouTube (#udp443) ----------------

func udp443Add(body []string) []string {
	if hasLine(body, "--filter-udp=443") {
		return body
	}
	var rest []string
	for _, l := range body {
		if !udp443MarkRx.MatchString(l) {
			rest = append(rest, l)
		}
	}
	AddPorts("", "443")
	return append(udp443Block(), rest...)
}

func SetYoutubeQuic(on bool) error {
	Mu.Lock()
	defer Mu.Unlock()
	if err := Guard(); err != nil {
		return err
	}
	return mutate(func(body []string) ([]string, error) {
		if on {
			AddGPDomains()
			return Normalize(udp443Add(body)), nil
		}
		out, _ := DropProfiles(body, udp443Rx, nil)
		var clean []string
		for _, l := range out {
			if !udp443MarkRx.MatchString(l) {
				clean = append(clean, l)
			}
		}
		return clean, nil
	})
}

// ---------------- helpers that keep YouTube / Discord blocks across strategy changes ----------------

func addYvDefault(body []string) []string {
	if app.S().YvOff {
		return body
	}
	for _, l := range body {
		if strings.HasPrefix(l, "#Yv") || strings.HasPrefix(l, "#general") {
			return body
		}
	}
	return append(yvDefault(), body...)
}

func discordStrAdd(body []string) []string {
	if app.S().DvOff {
		return body
	}
	AddPorts(DiscordTCP, DiscordUDP)
	for _, l := range body {
		if strings.Contains(l, "--filter-udp="+DiscordUDP) {
			return body
		}
	}
	return append(body, discordBlock()...)
}

func xtremeUndo(body []string) {
	if !anyMatch(body, gvXtRx) {
		app.Update(func(s *app.Settings) { s.XtremeBackup = nil })
		return
	}
	s := app.S()
	if len(s.XtremeBackup) == 5 {
		app.Update(func(st *app.Settings) {
			if s.XtremeBackup[3] != "" {
				st.PortsTCP = s.XtremeBackup[3]
			}
			if s.XtremeBackup[4] != "" {
				st.PortsUDP = s.XtremeBackup[4]
			}
		})
	}
	app.Update(func(st *app.Settings) { st.XtremeBackup = nil })
}

type keepState struct {
	yv     string
	dv     int
	gv     int
	xt     bool
	quic   bool
	wss    bool
	hasYv  bool
	hasDv  bool
	hasGv  bool
	body   []string
	ytFile string
}

func captureKeep(body []string) keepState {
	k := keepState{body: body, quic: hasLine(body, "--filter-udp=443"), wss: hasLine(body, wssLine)}
	if m := firstMatch(body, yvMarkRx); m != nil {
		k.yv, k.hasYv = m[1], true
	}
	if m := firstMatch(body, dvMarkRx); m != nil {
		k.dv, k.hasDv = atoi(m[1]), true
	}
	if m := firstMatch(body, gvMarkRx); m != nil {
		k.gv, k.hasGv = atoi(m[1]), true
	}
	k.xt = anyMatch(body, gvXtRx)
	return k
}

// SetV applies built-in strategy v1..v10 while keeping YouTube/Discord/game/QUIC/RKN/wssize
// choices (strategy_set_v).
func SetV(n int) error {
	if n < 1 || n > 10 {
		return fmt.Errorf("некорректная версия")
	}
	if !Installed() {
		return ErrNotInstalled
	}
	if err := Guard(); err != nil {
		return err
	}
	k := captureKeep(LoadBody())
	xtremeUndo(k.body)
	RemovePorts(PortsGameTCP, PortsGameUDP)
	app.Update(func(s *app.Settings) { s.FlowsealName = "" })
	body := StrategyV(n)
	AddGPDomains()
	_ = RefreshExclude(context.Background())
	body = addYvDefault(body)
	body = discordStrAdd(body)
	s := app.S()
	if k.hasYv && k.yv != "Yv08" && !s.YvOff {
		if !app.Exists(youtubeFile()) {
			_ = DownloadYoutube(context.Background(), nil)
		}
		if blk := youtubeBlock(k.yv); blk != nil {
			body = applyYoutube(body, k.yv, blk)
		}
	}
	if k.hasDv && k.dv != 1 && !s.DvOff {
		body = applyDv(body, k.dv)
	}
	if k.hasGv {
		body = applyGame(body, k.gv)
		if k.xt {
			body, _ = toggleXtreme(body)
		}
	}
	if k.quic {
		body = udp443Add(body)
	}
	body = reapplyOpts(body, k.wss)
	body = Normalize(body)
	if err := SaveBody(body); err != nil {
		return err
	}
	return Restart()
}

func reapplyOpts(body []string, wss bool) []string {
	if app.S().RknOn {
		if b, ok := rknPatch(body); ok {
			body = b
		}
	}
	if wss {
		body = wssAdd(body)
	}
	return body
}

// ---------------- Flowseal strategies ----------------

func FlowsealList() []string { return blockNames(app.ReadText(flowsealFile())) }

func blockNames(text string) []string {
	var out []string
	for _, l := range Lines(text) {
		if strings.HasPrefix(l, "#") {
			out = append(out, strings.TrimPrefix(l, "#"))
		}
	}
	return out
}

func findBlock(text, name string) []string {
	var out []string
	in := false
	for _, l := range Lines(text) {
		if l == "#"+name {
			in = true
			out = append(out, l)
			continue
		}
		if in && strings.HasPrefix(l, "#") {
			break
		}
		if in {
			out = append(out, l)
		}
	}
	return out
}

// SetFlowseal applies one Flowseal "general*" strategy (strategy_set_flowseal).
func SetFlowseal(name string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	if err := Guard(); err != nil {
		return err
	}
	block := findBlock(app.ReadText(flowsealFile()), name)
	if len(block) == 0 {
		return fmt.Errorf("стратегия не найдена — обновите список Flowseal")
	}
	k := captureKeep(LoadBody())
	app.Update(func(s *app.Settings) { s.YvOff, s.DvOff = false, false })
	xtremeUndo(k.body)
	RemovePorts(PortsGameTCP, PortsGameUDP)
	app.Update(func(s *app.Settings) { s.FlowsealName = name })
	AddPorts(DiscordTCP, DiscordUDP)
	AddGPDomains()
	_ = RefreshExclude(context.Background())
	// mark the game profile so game_status recognises it (sed '/--new/{N;/--filter-tcp=2802/...}')
	var body []string
	for i, l := range block {
		if l == "--new" && i+1 < len(block) && strings.Contains(block[i+1], "--filter-tcp=2802") {
			body = append(body, "#Gv0")
		}
		body = append(body, l)
	}
	for _, l := range block {
		if l == "--filter-udp="+PortsGameUDP || l == "--filter-tcp="+PortsGameTCP {
			AddPorts(PortsGameTCP, PortsGameUDP)
			break
		}
	}
	if k.quic {
		body = udp443Add(body)
	}
	body = reapplyOpts(body, k.wss)
	if err := SaveBody(body); err != nil {
		return err
	}
	return Restart()
}

// ---------------- YouTube strategies (#YvNN) ----------------

func YoutubeList() []string {
	var out []string
	for _, n := range blockNames(app.ReadText(youtubeFile())) {
		if strings.HasPrefix(n, "Yv") {
			out = append(out, n)
		}
	}
	return out
}

func youtubeBlock(name string) []string {
	b := findBlock(app.ReadText(youtubeFile()), name)
	if len(b) == 0 {
		return nil
	}
	return b[1:]
}

// applyYoutube removes the current YouTube TCP profile and puts the chosen one first.
func applyYoutube(body []string, name string, saved []string) []string {
	var stripped []string
	for i := 0; i < len(body); i++ {
		l := body[i]
		if yvMarkRx.MatchString(l) {
			continue
		}
		if l == "--filter-tcp=443" && i+1 < len(body) && body[i+1] == "--hostlist="+GoogleList {
			i++
			for i+1 < len(body) {
				i++
				if body[i] == "--new" {
					break
				}
			}
			continue
		}
		stripped = append(stripped, l)
	}
	out := append([]string{"#" + name}, saved...)
	out = append(out, "--new")
	out = append(out, stripped...)
	return Normalize(DedupNew(out))
}

func SetYoutube(name string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	if err := Guard(); err != nil {
		return err
	}
	if name == "off" {
		return mutate(func(body []string) ([]string, error) {
			out, _ := DropProfiles(body, yvRx, udp443Rx)
			app.Update(func(s *app.Settings) { s.YvOff = true })
			return out, nil
		})
	}
	blk := youtubeBlock(name)
	if blk == nil {
		return fmt.Errorf("стратегия не найдена — обновите список")
	}
	app.Update(func(s *app.Settings) { s.YvOff = false })
	AddGPDomains()
	return mutate(func(body []string) ([]string, error) { return applyYoutube(body, name, blk), nil })
}

func DownloadYoutube(ctx context.Context, j *app.Job) error {
	if j != nil {
		j.Say("==> Скачиваем список стратегий для YouTube")
	}
	t, err := app.FetchText(ctx, YoutubeStrURL, 30*time.Second)
	if err != nil {
		return fmt.Errorf("ошибка скачивания: %v", err)
	}
	t = NormalizePaths(t)
	if err := app.WriteText(youtubeFile(), t); err != nil {
		return err
	}
	if j != nil {
		j.Say("==> Готово, стратегий: %d", len(YoutubeList()))
	}
	return nil
}

// ---------------- Discord (#DvN) ----------------

type DiscordInfo struct {
	Current   string   `json:"current"`
	Fake      string   `json:"current_fake"`
	Active    bool     `json:"active"`
	Available []string `json:"available"`
	Fakes     []string `json:"fakes"`
}

func DiscordStatus() DiscordInfo {
	body := LoadBody()
	d := DiscordInfo{Fakes: FakeChoices}
	for i := 1; i <= 17; i++ {
		d.Available = append(d.Available, fmt.Sprintf("Dv%d", i))
	}
	if m := firstMatch(body, dvMarkRx); m != nil {
		d.Current = "Dv" + m[1]
	}
	for _, l := range body {
		if fakeDiscRx.MatchString(l) {
			d.Fake = l[strings.LastIndex(l, "/")+1:]
			break
		}
	}
	d.Active = hasLine(body, "--filter-l7=discord,stun") || anyMatch(body, dvTCPLine)
	return d
}

func applyDv(body []string, n int) []string {
	start := -1
	for i, l := range body {
		if dvTCPLine.MatchString(l) {
			start = i
			break
		}
	}
	if start < 0 {
		return body
	}
	end := len(body)
	for i := start + 1; i < len(body); i++ {
		if body[i] == "--new" || strings.HasPrefix(body[i], "#") {
			end = i
			break
		}
	}
	out := append([]string{}, body[:start]...)
	out = append(out, StrategyDv(n)...)
	out = append(out, body[end:]...)
	marked := false
	for i, l := range out {
		if dvMarkRx.MatchString(l) {
			out[i] = fmt.Sprintf("#Dv%d", n)
			marked = true
		}
	}
	if !marked {
		out = append(out[:start], append([]string{fmt.Sprintf("#Dv%d", n)}, out[start:]...)...)
	}
	return out
}

func SetDiscord(num string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	if err := Guard(); err != nil {
		return err
	}
	if num == "off" {
		return mutate(func(body []string) ([]string, error) {
			out, removed := DropProfiles(body, discordRx, nil)
			if removed {
				RemovePorts(DiscordTCP, DiscordUDP)
			}
			app.Update(func(s *app.Settings) { s.DvOff = true })
			return out, nil
		})
	}
	n := atoi(strings.TrimPrefix(num, "Dv"))
	if n < 1 || n > 17 {
		return fmt.Errorf("некорректный номер Dv")
	}
	return mutate(func(body []string) ([]string, error) {
		if !hasLine(body, "--filter-l7=discord,stun") || !anyMatch(body, dvTCPLine) {
			body, _ = DropProfiles(body, discordRx, nil)
			app.Update(func(s *app.Settings) { s.DvOff = false })
			body = discordStrAdd(body)
			if !anyMatch(body, dvTCPLine) {
				return nil, fmt.Errorf("не удалось добавить блок Discord — сначала выберите основную стратегию")
			}
		}
		app.Update(func(s *app.Settings) { s.DvOff = false })
		return applyDv(body, n), nil
	})
}

func validFake(f string) bool {
	for _, x := range FakeChoices {
		if x == f {
			return true
		}
	}
	return false
}

func SetDiscordFake(file string) error {
	if !validFake(file) {
		return fmt.Errorf("неизвестный fake-файл")
	}
	if err := Guard(); err != nil {
		return err
	}
	return mutate(func(body []string) ([]string, error) {
		if !hasLine(body, "--filter-l7=discord,stun") {
			return nil, fmt.Errorf("стратегия Discord выключена — сначала выберите Dv")
		}
		out := append([]string{}, body...)
		for i := 0; i+3 < len(out); i++ {
			if out[i] == "--filter-l7=discord,stun" && out[i+1] == "--dpi-desync=fake" && fakeDiscRx.MatchString(out[i+2]) {
				out[i+2] = "--dpi-desync-fake-discord=" + FK + file
				out[i+3] = "--dpi-desync-fake-stun=" + FK + file
			}
		}
		return out, nil
	})
}

// ---------------- Games (#GvN, Xtreme) ----------------

type GameInfo struct {
	Current string   `json:"current"`
	Xtreme  bool     `json:"xtreme"`
	Fake    string   `json:"fake"`
	Active  bool     `json:"active"`
	Fakes   []string `json:"fakes"`
}

func GameStatus() GameInfo {
	body := LoadBody()
	g := GameInfo{Fakes: FakeChoices, Xtreme: anyMatch(body, gvXtRx), Active: anyMatch(body, gameRx)}
	if m := firstMatch(body, gvMarkRx); m != nil {
		g.Current = "Gv" + m[1]
	}
	for _, l := range body {
		if strings.HasPrefix(l, "--dpi-desync-fake-unknown-udp=") {
			g.Fake = l[strings.LastIndex(l, "/")+1:]
			break
		}
	}
	return g
}

func applyGame(body []string, n int) []string {
	out, _ := DropProfiles(body, gameRx, nil)
	out = append(out, strategyGv(n)...)
	out = append(out, strategyTCPCommon()...)
	AddPorts(PortsGameTCP, PortsGameUDP)
	return out
}

func SetGame(choice string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	if err := Guard(); err != nil {
		return err
	}
	body := LoadBody()
	if choice == "off" {
		if !anyMatch(body, gameRx) {
			return nil
		}
		xtremeUndo(body)
		out, _ := DropProfiles(body, gameRx, nil)
		RemovePorts(PortsGameTCP, PortsGameUDP)
		if err := SaveBody(out); err != nil {
			return err
		}
		return Restart()
	}
	n := atoi(strings.TrimPrefix(choice, "Gv"))
	if n < 1 || n > 4 {
		return fmt.Errorf("некорректный номер Gv")
	}
	if m := firstMatch(body, gvMarkRx); m != nil && atoi(m[1]) == n && m[2] == "" {
		return nil
	}
	xtremeUndo(body)
	return mutate(func(body []string) ([]string, error) { return applyGame(body, n), nil })
}

func SetGameFake(file string) error {
	if !validFake(file) {
		return fmt.Errorf("неизвестный fake-файл")
	}
	if err := Guard(); err != nil {
		return err
	}
	return mutate(func(body []string) ([]string, error) {
		out := append([]string{}, body...)
		for i, l := range out {
			if strings.HasPrefix(l, "--dpi-desync-fake-unknown-udp=") {
				out[i] = "--dpi-desync-fake-unknown-udp=" + FK + file
				return out, nil
			}
		}
		return nil, fmt.Errorf("игровая стратегия не установлена")
	})
}

// toggleXtreme widens the game profile to all ports and back (game_toggle_xtreme).
func toggleXtreme(body []string) ([]string, error) {
	s := app.S()
	out := append([]string{}, body...)
	if anyMatch(body, gvXtRx) {
		b := s.XtremeBackup
		if len(b) != 5 {
			return nil, fmt.Errorf("данные для восстановления отсутствуют")
		}
		restore := false
		for i, l := range out {
			switch {
			case gvXtRx.MatchString(l):
				out[i] = b[0]
				restore = true
			case restore && strings.HasPrefix(l, "--filter-udp="):
				out[i] = b[1]
			case restore && strings.HasPrefix(l, "--filter-tcp="):
				out[i] = b[2]
				restore = false
			}
		}
		app.Update(func(st *app.Settings) {
			if b[3] != "" {
				st.PortsTCP = b[3]
			}
			if b[4] != "" {
				st.PortsUDP = b[4]
			}
			st.XtremeBackup = nil
		})
		return out, nil
	}
	if !anyMatch(body, gvPlainRx) {
		return nil, fmt.Errorf("игровая стратегия не установлена")
	}
	var gv, udp, tcp string
	found := false
	for _, l := range body {
		switch {
		case gvPlainRx.MatchString(l) && !found:
			gv, found = l, true
		case found && strings.HasPrefix(l, "--filter-udp="):
			udp = l
		case found && strings.HasPrefix(l, "--filter-tcp="):
			tcp = l
		}
	}
	app.Update(func(st *app.Settings) {
		st.XtremeBackup = []string{gv, udp, tcp, st.PortsTCP, st.PortsUDP}
		st.PortsTCP, st.PortsUDP = XtremeNfqwsPorts, XtremeNfqwsPorts
	})
	gvMode := false
	for i, l := range out {
		switch {
		case gvPlainRx.MatchString(l):
			out[i] = l + "Xtreme"
			gvMode = true
		case gvMode && strings.HasPrefix(l, "--filter-udp="):
			out[i] = "--filter-udp=" + XtremePorts
		case gvMode && strings.HasPrefix(l, "--filter-tcp="):
			out[i] = "--filter-tcp=" + XtremePorts
			gvMode = false
		}
	}
	return out, nil
}

func ToggleXtreme() error {
	if err := Guard(); err != nil {
		return err
	}
	return mutate(toggleXtreme)
}

// ---------------- RKN list + --wssize ----------------

func rknOn(body []string) bool { return hasLine(body, "--hostlist="+RknList) }

func rknPatch(body []string) ([]string, bool) {
	if countLines(ListPath(RknList)) == 0 {
		return body, false
	}
	if rknOn(body) {
		return body, true
	}
	if !hasLine(body, exclHL) {
		return body, false
	}
	if !app.Exists(ListPath(UserList)) {
		_ = app.WriteText(ListPath(UserList), "")
	}
	var out []string
	for _, l := range body {
		if l == exclHL {
			out = append(out, "--hostlist="+UserList, "--hostlist="+RknList)
			continue
		}
		out = append(out, l)
	}
	return out, true
}

func rknUnpatch(body []string) []string {
	var out []string
	held := false
	for _, l := range body {
		switch {
		case l == "--hostlist="+UserList && !held:
			held = true
			continue
		case l == "--hostlist="+RknList:
			if held {
				out = append(out, exclHL)
				held = false
			}
			continue
		case held:
			out = append(out, "--hostlist="+UserList)
			held = false
		}
		out = append(out, l)
	}
	if held {
		out = append(out, "--hostlist="+UserList)
	}
	return out
}

func wssAdd(body []string) []string {
	if hasLine(body, wssLine) {
		return body
	}
	real := false
	for _, l := range body {
		if strings.HasPrefix(l, "--") {
			real = true
		}
	}
	if !real {
		return body
	}
	return append(body, "--new", "--filter-tcp=443", wssLine)
}

// SetOpt toggles extras: rkn:on|off, wssize:on|off (zapret_opt_set).
func SetOpt(what, val string, ctx context.Context) error {
	if !Installed() {
		return ErrNotInstalled
	}
	switch what {
	case "rkn":
		if err := Guard(); err != nil {
			return err
		}
		if val == "on" {
			body := LoadBody()
			if !rknOn(body) {
				if !hasLine(body, exclHL) {
					return fmt.Errorf("текущая стратегия не подходит для списков РКН — выберите одну из v1–v10")
				}
				t, err := app.FetchText(ctx, RknURL, 40*time.Second)
				if err != nil || strings.Count(t, "\n") < 100 {
					return fmt.Errorf("не удалось скачать список РКН — попробуйте позже")
				}
				if err := app.WriteText(ListPath(RknList), t); err != nil {
					return err
				}
			}
			app.Update(func(s *app.Settings) { s.RknOn = true })
			return mutate(func(b []string) ([]string, error) {
				out, ok := rknPatch(b)
				if !ok {
					return nil, fmt.Errorf("не удалось включить списки РКН в стратегии")
				}
				return out, nil
			})
		}
		app.Update(func(s *app.Settings) { s.RknOn = false })
		err := mutate(func(b []string) ([]string, error) { return rknUnpatch(b), nil })
		app.Remove(ListPath(RknList))
		return err
	case "wssize":
		if err := Guard(); err != nil {
			return err
		}
		if val == "on" {
			return mutate(func(b []string) ([]string, error) {
				out := wssAdd(b)
				if !hasLine(out, wssLine) {
					return nil, fmt.Errorf("не удалось добавить блок --wssize: в стратегии нет ни одного профиля")
				}
				return out, nil
			})
		}
		return mutate(func(b []string) ([]string, error) { out, _ := DropProfiles(b, wssRx, nil); return out, nil })
	}
	return fmt.Errorf("неизвестная настройка")
}

// ---------------- raw editor + nochange ----------------

func GetOpt() string { return Join(LoadBody()) }

// SetRaw saves a hand-edited strategy (nfqws_opt_set). Router paths are converted on the fly.
func SetRaw(content string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	content = NormalizePaths(strings.ReplaceAll(content, "\r", ""))
	if strings.Contains(content, "'") {
		return fmt.Errorf("одинарные кавычки ' в стратегии нельзя")
	}
	if !strings.Contains(content, "--") {
		return fmt.Errorf("в стратегии нет ни одного параметра --… — сохранение отменено")
	}
	// accept a whole router UCI block pasted as-is
	if i := strings.Index(content, "option NFQWS_OPT '"); i >= 0 {
		content = content[i+len("option NFQWS_OPT '"):]
		if k := strings.Index(content, "'"); k >= 0 {
			content = content[:k]
		}
	}
	app.Update(func(s *app.Settings) { s.FlowsealName = "" })
	return mutate(func([]string) ([]string, error) { return Lines(content), nil })
}

func SetNochange(on bool) error {
	body := LoadBody()
	if on {
		if !Nochange(Join(body)) {
			body = append([]string{"#nochange"}, body...)
		}
	} else {
		body = StripNochange(body)
		if Nochange(Join(body)) {
			return fmt.Errorf("пометка #nochange стоит внутри другой строки — уберите её вручную в редакторе стратегии")
		}
	}
	return SaveBody(body)
}

// ExportRouter returns the strategy in OpenWrt form, ready to paste into /etc/config/zapret.
func ExportRouter() string {
	s := app.S()
	return fmt.Sprintf("\toption NFQWS_PORTS_TCP '%s'\n\toption NFQWS_PORTS_UDP '%s'\n\toption NFQWS_OPT '\n%s\n'\n",
		s.PortsTCP, s.PortsUDP, RouterPaths(Join(LoadBody())))
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
