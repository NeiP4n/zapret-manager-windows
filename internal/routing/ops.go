package routing

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

type StatusInfo struct {
	Installed bool          `json:"installed"`
	UI        bool          `json:"ui"`
	UIURL     string        `json:"ui_url,omitempty"`
	Mode      string        `json:"mode"`
	Running   bool          `json:"running"`
	Proc      osx.ProcState `json:"proc"`
	Config    Config        `json:"config"`
	Services  []Service     `json:"services"`
	Warp      int           `json:"warp_accounts"`
	Log       []string      `json:"log"`
	ListsAt   int64         `json:"lists_at"`
}

func Status() StatusInfo {
	c := Load()
	st := StatusInfo{Installed: Installed(), UI: UIInstalled(), Mode: c.mode(), Proc: osx.ProcStatus(procName),
		Config: c, Services: Services, Warp: len(loadWarp()), Log: osx.TailLog(procName, 12), ListsAt: c.ListsAt}
	st.Running = st.Proc.Running
	st.Config.Secret = ""
	if st.UI {
		st.UIURL = fmt.Sprintf("http://127.0.0.1:%d/ui/#/setup?hostname=127.0.0.1&port=%d&secret=%s", c.CtrlPort, c.CtrlPort, c.Secret)
	}
	return st
}

// apply saves config, refreshes missing lists and restarts the engine.
func apply(ctx context.Context, c Config, say func(string, ...any)) error {
	mu.Lock()
	err := save(c)
	mu.Unlock()
	if err != nil {
		return err
	}
	if !Installed() {
		return nil
	}
	if say == nil {
		say = func(string, ...any) {}
	}
	if err := UpdateLists(ctx, say, false); err != nil {
		say("!! %v", err)
	}
	return Restart()
}

// ---------------- Forkozz sections ----------------

func SaveSection(ctx context.Context, s Section) (Section, error) {
	mu.Lock()
	c := Load()
	mu.Unlock()
	if s.ID == "" {
		s.ID = newSectionID(c)
	}
	if err := s.Validate(); err != nil {
		return s, err
	}
	found := false
	for i := range c.Sections {
		if c.Sections[i].ID == s.ID {
			c.Sections[i] = s
			found = true
		}
	}
	if !found {
		if len(c.Sections) >= 20 {
			return s, fmt.Errorf("слишком много секций")
		}
		c.Sections = append(c.Sections, s)
	}
	return s, apply(ctx, c, nil)
}

func DeleteSection(ctx context.Context, id string) error {
	mu.Lock()
	c := Load()
	mu.Unlock()
	var out []Section
	for _, s := range c.Sections {
		if s.ID != id {
			out = append(out, s)
		}
	}
	c.Sections = out
	return apply(ctx, c, nil)
}

// MoveSection changes priority (rules are matched in section order).
func MoveSection(ctx context.Context, id string, dir int) error {
	mu.Lock()
	c := Load()
	mu.Unlock()
	for i := range c.Sections {
		if c.Sections[i].ID == id {
			k := i + dir
			if k < 0 || k >= len(c.Sections) {
				return nil
			}
			c.Sections[i], c.Sections[k] = c.Sections[k], c.Sections[i]
			break
		}
	}
	return apply(ctx, c, nil)
}

type Globals struct {
	Exclude   string `json:"exclude"`
	DNSMode   string `json:"dns_mode"`
	ListsIv   string `json:"lists_interval"`
	BlockQUIC bool   `json:"block_quic"`
}

func SetGlobals(ctx context.Context, g Globals) error {
	for _, x := range splitList(g.Exclude) {
		if net.ParseIP(strings.Split(x, "/")[0]) == nil && !domRx.MatchString(normDomain(x)) {
			return fmt.Errorf("исключения: не похоже на домен или адрес: %s", x)
		}
	}
	mu.Lock()
	c := Load()
	mu.Unlock()
	c.Exclude, c.BlockQUIC = g.Exclude, g.BlockQUIC
	if g.DNSMode == "fake-ip" || g.DNSMode == "redir-host" {
		c.DNSMode = g.DNSMode
	}
	switch g.ListsIv {
	case "1h", "3h", "12h", "1d", "3d", "off":
		c.ListsIv = g.ListsIv
	}
	return apply(ctx, c, nil)
}

// ---------------- Steer ----------------

func SaveSteer(ctx context.Context, s Steer, say func(string, ...any)) error {
	if s.Tunnels < 1 || s.Tunnels > 3 {
		s.Tunnels = 2
	}
	for _, id := range s.Services {
		if _, ok := serviceByID(id); !ok {
			return fmt.Errorf("неизвестный сервис %s", id)
		}
	}
	for _, d := range splitList(s.Domains) {
		if !domRx.MatchString(normDomain(d)) {
			return fmt.Errorf("не похоже на домен: %s", d)
		}
	}
	if s.Enabled && !Installed() {
		return fmt.Errorf("сначала установите движок (Mihomo)")
	}
	if s.Enabled {
		if say == nil {
			say = func(string, ...any) {}
		}
		if err := EnsureWarp(ctx, s.Tunnels, false, say); err != nil {
			return err
		}
	}
	mu.Lock()
	c := Load()
	mu.Unlock()
	c.Steer = s
	return apply(ctx, c, say)
}

func RecreateWarp(ctx context.Context, say func(string, ...any)) error {
	c := Load()
	if err := EnsureWarp(ctx, c.Steer.Tunnels, true, say); err != nil {
		return err
	}
	return Restart()
}

// ---------------- Mixomo ----------------

func MixomoGet() string { return app.ReadText(MixomoConf()) }

func MixomoSet(text string) error {
	text = strings.ReplaceAll(text, "\r", "")
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("пустая конфигурация")
	}
	low := strings.ToLower(text)
	if !strings.Contains(low, "proxies") && !strings.Contains(low, "proxy-providers") && !strings.Contains(low, "rules") {
		return fmt.Errorf("не похоже на конфигурацию mihomo: нет proxies / proxy-providers / rules")
	}
	if !strings.Contains(low, "tun:") {
		text += "\n# добавлено Zapret Manager: без TUN трафик не перехватывается\ntun:\n  enable: true\n  stack: mixed\n  device: " + tunName + "\n  auto-route: true\n  auto-detect-interface: true\n  dns-hijack:\n    - any:53\n    - tcp://any:53\n"
	}
	_ = os.MkdirAll(Dir(), 0o755)
	old := MixomoGet()
	if err := app.WriteText(MixomoConf(), text); err != nil {
		return err
	}
	if Load().Mixomo.Enabled {
		if err := Restart(); err != nil {
			_ = app.WriteText(MixomoConf(), old)
			_ = Restart()
			return err
		}
	}
	return nil
}

// MixomoGenerate writes a config from the subscription + list preset (router Mixomo install).
func MixomoGenerate(ctx context.Context, m Mixomo, say func(string, ...any)) error {
	mu.Lock()
	c := Load()
	mu.Unlock()
	c.Mixomo.SubURL, c.Mixomo.Filter, c.Mixomo.ListPreset, c.Mixomo.WarpProxy = m.SubURL, m.Filter, m.ListPreset, m.WarpProxy
	if c.Mixomo.WarpProxy {
		if err := EnsureWarp(ctx, 1, false, say); err != nil {
			return err
		}
	}
	for _, id := range strings.Split(c.Mixomo.ListPreset, ",") {
		if s, ok := serviceByID(id); ok {
			if t, err := app.FetchText(ctx, s.Domains, 40*time.Second); err == nil {
				_ = os.MkdirAll(rulesDir(), 0o755)
				_ = app.WriteText(listFile(id), convertDomains(t))
			}
			if s.Subnets != "" {
				if t, err := app.FetchText(ctx, s.Subnets, 40*time.Second); err == nil {
					_ = app.WriteText(subnetFile(id), convertCIDR(t))
				}
			}
		}
	}
	b := jsonPretty(MixomoTemplate(c))
	if err := app.WriteText(MixomoConf(), b); err != nil {
		return err
	}
	c.Mixomo.Enabled = true
	return apply(ctx, c, say)
}

func MixomoSettings(ctx context.Context, m Mixomo) error {
	mu.Lock()
	c := Load()
	mu.Unlock()
	if m.Enabled && !app.Exists(MixomoConf()) {
		return fmt.Errorf("нет конфигурации Mixomo — вставьте свою или создайте по подписке")
	}
	if m.AutoRestart != "" {
		var h, mm int
		if n, _ := fmt.Sscanf(m.AutoRestart, "%d:%d", &h, &mm); n != 2 || h > 23 || mm > 59 {
			return fmt.Errorf("время перезапуска — ЧЧ:ММ")
		}
	}
	c.Mixomo.Enabled, c.Mixomo.AutoRestart = m.Enabled, m.AutoRestart
	return apply(ctx, c, nil)
}

// SetSubscription replaces the subscription URL inside the stored Mixomo template.
func SetSubscription(ctx context.Context, u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("ссылка на подписку должна начинаться с http(s)://")
	}
	c := Load()
	m := c.Mixomo
	m.SubURL = u
	return MixomoGenerate(ctx, m, nil)
}

// ---------------- explain ----------------

type Explain struct {
	Target string `json:"target"`
	Route  string `json:"route"`
	Reason string `json:"reason"`
}

func fileHas(path, domain string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimPrefix(sc.Text(), "+.")
		if l == domain || strings.HasSuffix(domain, "."+l) {
			return true
		}
	}
	return false
}

func fileHasIP(path string, ip net.IP) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if _, n, err := net.ParseCIDR(strings.TrimSpace(sc.Text())); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func matchCustom(list, domain string, ip net.IP) bool {
	for _, x := range splitList(list) {
		if ip != nil {
			if !strings.Contains(x, "/") {
				x += "/32"
			}
			if _, n, err := net.ParseCIDR(x); err == nil && n.Contains(ip) {
				return true
			}
			continue
		}
		d := normDomain(x)
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// ExplainRoute tells where a site goes — Steer "куда пойдёт сайт" / Forkozz route check.
func ExplainRoute(target string) Explain {
	c := Load()
	t := normDomain(target)
	ip := net.ParseIP(t)
	r := Explain{Target: t}
	if c.mode() == "off" {
		r.Route, r.Reason = "напрямую", "маршрутизация выключена"
		return r
	}
	if c.mode() == "mixomo" {
		r.Route, r.Reason = "по правилам Mixomo", "используется своя конфигурация mihomo — смотрите её правила"
		return r
	}
	if matchCustom(c.Exclude, t, ip) {
		r.Route, r.Reason = "напрямую", "адрес в исключениях — идёт мимо VPN"
		return r
	}
	check := func(services []string, domains, subnets string) string {
		if matchCustom(domains, t, ip) {
			return "свой список доменов"
		}
		if ip != nil && matchCustom(subnets, t, ip) {
			return "свои подсети"
		}
		for _, id := range services {
			s, _ := serviceByID(id)
			if ip == nil && fileHas(listFile(id), t) {
				return "список «" + s.Title + "»"
			}
			if ip != nil && fileHasIP(subnetFile(id), ip) {
				return "подсети «" + s.Title + "»"
			}
		}
		return ""
	}
	for _, s := range c.Sections {
		if !s.Enabled {
			continue
		}
		if why := check(s.Services, s.Domains, s.Subnets); why != "" {
			r.Route, r.Reason = "Forkozz «"+s.Name+"»", why
			return r
		}
	}
	if c.Steer.Enabled {
		if why := check(c.Steer.Services, c.Steer.Domains, ""); why != "" {
			r.Route, r.Reason = "Steer → WARP", why
			return r
		}
	}
	r.Route, r.Reason = "напрямую", "не попадает ни в один список"
	return r
}

// ---------------- scheduler hooks ----------------

// Tick refreshes lists by interval and performs the Mixomo daily restart.
func Tick(ctx context.Context, now time.Time) {
	c := Load()
	if c.mode() == "off" || !Installed() {
		return
	}
	iv := map[string]time.Duration{"1h": time.Hour, "3h": 3 * time.Hour, "12h": 12 * time.Hour, "1d": 24 * time.Hour, "3d": 72 * time.Hour}[c.ListsIv]
	if iv > 0 && time.Since(time.Unix(c.ListsAt, 0)) > iv && c.mode() == "generated" {
		if UpdateLists(ctx, nil, true) == nil {
			_ = Restart()
		}
	}
	if c.Mixomo.Enabled && c.Mixomo.AutoRestart != "" && now.Format("15:04") == c.Mixomo.AutoRestart && c.Mixomo.LastRestart != now.Format("2006-01-02") {
		mu.Lock()
		c = Load()
		c.Mixomo.LastRestart = now.Format("2006-01-02")
		_ = save(c)
		mu.Unlock()
		_ = Restart()
	}
}

// SetEngine turns the whole engine on/off without losing settings.
func SetEngine(on bool) error {
	if !on {
		Stop()
		return nil
	}
	return Restart()
}
