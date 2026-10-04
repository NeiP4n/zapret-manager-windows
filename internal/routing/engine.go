package routing

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/awg"
	"github.com/zapretmanager/zmwin/internal/osx"
)

const (
	repo     = "MetaCubeX/mihomo"
	uiRepo   = "MetaCubeX/metacubexd"
	procName = "mihomo"
	tunName  = "ZapretManager"
)

func Dir() string          { return filepath.Join(app.ToolsDir, "mihomo") }
func exe() string          { return filepath.Join(Dir(), "mihomo.exe") }
func rulesDir() string     { return filepath.Join(Dir(), "rules") }
func provDir() string      { return filepath.Join(Dir(), "providers") }
func genConf() string      { return filepath.Join(Dir(), "config.json") }
func MixomoConf() string   { return filepath.Join(Dir(), "mixomo.yaml") }
func uiDir() string        { return filepath.Join(Dir(), "ui") }
func Installed() bool      { return app.Exists(exe()) || (app.Dev && Load().Version != "") }
func UIInstalled() bool    { return app.Exists(filepath.Join(uiDir(), "index.html")) }
func warpKeysPath() string { return app.P("state", "routing_warp.json") }

// ---------------- install ----------------

func Install(j *app.Job) error {
	ctx := j.Ctx()
	j.Say("==> Узнаём последнюю версию Mihomo")
	tag := app.LatestTag(ctx, repo)
	if tag == "" {
		tag = "v1.19.32"
		j.Say("!! Не удалось узнать версию, используем %s", tag)
	}
	asset := fmt.Sprintf("mihomo-windows-amd64-compatible-%s.zip", tag)
	url := fmt.Sprintf("%s/%s/releases/download/%s/%s", app.GHMain, repo, tag, asset)
	tmp := filepath.Join(app.TmpDir, asset)
	j.Say("==> Скачиваем %s", asset)
	if err := app.Download(ctx, url, tmp, 4, j); err != nil {
		return err
	}
	defer os.Remove(tmp)
	osx.ProcStop(procName)
	if _, err := app.Unzip(tmp, Dir(), func(n string) string {
		if strings.HasSuffix(strings.ToLower(n), ".exe") {
			return "mihomo.exe"
		}
		return ""
	}); err != nil {
		return err
	}
	j.Say("==> Скачиваем wintun (драйвер TUN)")
	wz := filepath.Join(app.TmpDir, "wintun.zip")
	if err := app.Download(ctx, "https://www.wintun.net/builds/wintun-0.14.1.zip", wz, 3, j); err == nil {
		_, _ = app.Unzip(wz, Dir(), func(n string) string {
			if strings.HasSuffix(n, "bin/amd64/wintun.dll") {
				return "wintun.dll"
			}
			return ""
		})
		os.Remove(wz)
	} else {
		j.Say("   wintun не скачался — mihomo использует встроенный")
	}
	mu.Lock()
	c := Load()
	c.Version = tag
	if c.Secret == "" {
		c.Secret = randHex(12)
	}
	_ = save(c)
	mu.Unlock()
	j.Say("==> Готово, Mihomo %s установлен", tag)
	return nil
}

func InstallUI(j *app.Job) error {
	tag := app.LatestTag(j.Ctx(), uiRepo)
	if tag == "" {
		return fmt.Errorf("не удалось узнать версию metacubexd")
	}
	url := fmt.Sprintf("%s/%s/releases/download/%s/compressed-dist.tgz", app.GHMain, uiRepo, tag)
	tmp := filepath.Join(app.TmpDir, "metacubexd.tgz")
	j.Say("==> Скачиваем веб-интерфейс metacubexd %s", tag)
	if err := app.Download(j.Ctx(), url, tmp, 3, j); err != nil {
		return err
	}
	defer os.Remove(tmp)
	app.Remove(uiDir())
	if err := app.UnTarGz(tmp, uiDir(), 0); err != nil {
		return err
	}
	if !UIInstalled() {
		// archives sometimes wrap files in a folder
		if sub, _ := filepath.Glob(filepath.Join(uiDir(), "*", "index.html")); len(sub) > 0 {
			d := filepath.Dir(sub[0])
			entries, _ := os.ReadDir(d)
			for _, e := range entries {
				_ = os.Rename(filepath.Join(d, e.Name()), filepath.Join(uiDir(), e.Name()))
			}
		}
	}
	j.Say("==> Готово")
	return Restart()
}

func Remove(j *app.Job) error {
	osx.ProcStop(procName)
	app.Remove(Dir(), warpKeysPath())
	mu.Lock()
	_ = save(Config{})
	mu.Unlock()
	j.Say("==> Mihomo, списки и настройки маршрутизации удалены")
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------- lists ----------------

func listFile(id string) string   { return filepath.Join(rulesDir(), id+".txt") }
func subnetFile(id string) string { return filepath.Join(rulesDir(), id+"_ip.txt") }

// convertDomains turns a plain itdoginfo list into mihomo "domain" rule-provider text
// (+.example.com = the domain and all its subdomains).
func convertDomains(t string) string {
	var b strings.Builder
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "regexp:") {
			continue
		}
		l = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(l, "full:"), "domain:"), "*.")
		l = strings.TrimPrefix(l, ".")
		if !domRx.MatchString(strings.ToLower(l)) {
			continue
		}
		b.WriteString("+." + strings.ToLower(l) + "\n")
	}
	return b.String()
}

func convertCIDR(t string) string {
	var b strings.Builder
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if !strings.Contains(l, "/") {
			if ip := net.ParseIP(l); ip != nil {
				if ip.To4() != nil {
					l += "/32"
				} else {
					l += "/128"
				}
			}
		}
		if _, _, err := net.ParseCIDR(l); err == nil {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// UpdateLists downloads every list in use (router: do_fk_lists / steer lists).
func UpdateLists(ctx context.Context, say func(string, ...any), force bool) error {
	c := Load()
	_ = os.MkdirAll(rulesDir(), 0o755)
	var failed []string
	fetch := func(u, dst string, conv func(string) string) {
		if !force && app.Exists(dst) {
			return
		}
		t, err := app.FetchText(ctx, u, 40*time.Second)
		if err != nil {
			failed = append(failed, filepath.Base(dst))
			return
		}
		_ = app.WriteText(dst, conv(t))
	}
	for _, id := range c.usedServices() {
		s, _ := serviceByID(id)
		if say != nil {
			say("   · %s", s.Title)
		}
		fetch(s.Domains, listFile(id), convertDomains)
		if s.Subnets != "" {
			fetch(s.Subnets, subnetFile(id), convertCIDR)
		}
	}
	for _, s := range c.Sections {
		if !s.Enabled {
			continue
		}
		for i, u := range splitList(s.ListURLs) {
			dst := filepath.Join(rulesDir(), fmt.Sprintf("%s_ext%d.txt", s.ID, i))
			t, err := app.FetchText(ctx, u, 40*time.Second)
			if err != nil {
				failed = append(failed, u)
				continue
			}
			if strings.TrimSpace(convertCIDR(t)) != "" && strings.TrimSpace(convertDomains(t)) == "" {
				_ = app.WriteText(dst+".ip", convertCIDR(t))
				app.Remove(dst)
			} else {
				_ = app.WriteText(dst, convertDomains(t))
				app.Remove(dst + ".ip")
			}
		}
	}
	mu.Lock()
	c = Load()
	c.ListsAt = time.Now().Unix()
	_ = save(c)
	mu.Unlock()
	if len(failed) > 0 {
		return fmt.Errorf("не скачались: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ---------------- WARP accounts for Steer / "warp" sections ----------------

type warpAcc struct {
	Keys     awg.Keys `json:"keys"`
	Endpoint string   `json:"endpoint"`
}

func loadWarp() []warpAcc {
	var w []warpAcc
	b, _ := os.ReadFile(warpKeysPath())
	_ = json.Unmarshal(b, &w)
	return w
}

// EnsureWarp registers missing WARP accounts (Steer "подобрать три туннеля").
func EnsureWarp(ctx context.Context, n int, recreate bool, say func(string, ...any)) error {
	if n < 1 {
		n = 1
	}
	if n > 3 {
		n = 3
	}
	ws := loadWarp()
	if recreate {
		ws = nil
	}
	c := Load()
	ep := c.Steer.Endpoint
	if ep == "" {
		ep = "engage.cloudflareclient.com:4500"
	}
	for len(ws) < n {
		say("==> Получаем ключи WARP для туннеля %d", len(ws)+1)
		k, err := awg.Register(ctx, say)
		if err != nil {
			return err
		}
		ws = append(ws, warpAcc{Keys: *k, Endpoint: awg.Endpoints[len(ws)%len(awg.Endpoints)]})
	}
	ws = ws[:n]
	for i := range ws {
		if c.Steer.Endpoint != "" {
			ws[i].Endpoint = ep
		}
	}
	b, _ := json.MarshalIndent(ws, "", " ")
	return app.WriteFileAtomic(warpKeysPath(), b)
}

func warpProxies(c Config) []map[string]any {
	var out []map[string]any
	i1 := awg.I1(c.Steer.I1)
	for i, w := range loadWarp() {
		host, port, err := net.SplitHostPort(w.Endpoint)
		if err != nil {
			host, port = "engage.cloudflareclient.com", "4500"
		}
		p, _ := strconv.Atoi(port)
		opt := map[string]any{"jc": 4, "jmin": 40, "jmax": 70, "s1": 0, "s2": 0, "h1": 1, "h2": 2, "h3": 3, "h4": 4}
		if i1 != "" {
			opt["i1"] = i1
		}
		px := map[string]any{
			"name": fmt.Sprintf("WARP %d", i+1), "type": "wireguard", "server": host, "port": p,
			"ip": strings.Split(w.Keys.V4, "/")[0], "private-key": w.Keys.Priv, "public-key": w.Keys.Peer,
			"allowed-ips": []string{"0.0.0.0/0", "::/0"}, "mtu": 1280, "udp": true, "amnezia-wg-option": opt,
		}
		if w.Keys.V6 != "" {
			px["ipv6"] = strings.Split(w.Keys.V6, "/")[0]
		}
		out = append(out, px)
	}
	return out
}

// ---------------- config generation ----------------

type M = map[string]any

func groupName(s Section, used map[string]bool) string {
	n := strings.TrimSpace(s.Name)
	if n == "" || used[n] || n == "DIRECT" || n == "REJECT" || n == "WARP" {
		n = n + " (" + s.ID + ")"
	}
	used[n] = true
	return n
}

func dnsBlock(c Config) M {
	mode := c.DNSMode
	if mode != "redir-host" {
		mode = "fake-ip"
	}
	ns := []string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query", "https://common.dot.dns.yandex.net/dns-query"}
	return M{
		"enable": true, "ipv6": !app.S().DisableIPv6, "enhanced-mode": mode, "fake-ip-range": "198.18.0.1/16",
		"fake-ip-filter": []string{"*.lan", "*.local", "*.localdomain", "+.msftconnecttest.com", "+.msftncsi.com",
			"time.windows.com", "+.ntp.org", "+.pool.ntp.org", "stun.*.*", "stun.*.*.*", "+.stun.*.*", "localhost.ptlogin2.qq.com"},
		"default-nameserver":      []string{"77.88.8.8", "1.1.1.1", "8.8.8.8"},
		"nameserver":              ns,
		"proxy-server-nameserver": ns,
		"respect-rules":           false,
	}
}

func baseConfig(c Config) M {
	m := M{
		"mode": "rule", "log-level": "warning", "ipv6": !app.S().DisableIPv6, "unified-delay": true,
		"tcp-concurrent": true, "find-process-mode": "off", "allow-lan": false,
		"profile":      M{"store-selected": true, "store-fake-ip": true},
		"geodata-mode": false, "geo-auto-update": false,
		"sniffer": M{"enable": true, "force-dns-mapping": true, "parse-pure-ip": true, "override-destination": false,
			"sniff": M{"HTTP": M{"ports": []any{80, "8080-8880"}}, "TLS": M{"ports": []any{443, 8443}}, "QUIC": M{"ports": []any{443, 8443}}}},
		"tun": M{"enable": true, "stack": "mixed", "device": tunName, "auto-route": true, "auto-detect-interface": true,
			"strict-route": false, "dns-hijack": []string{"any:53", "tcp://any:53"}, "mtu": 1500},
		"dns": dnsBlock(c),
	}
	return m
}

// Generate compiles Forkozz sections + Steer into a mihomo config.
func Generate(c Config) (M, error) {
	m := baseConfig(c)
	var proxies []M
	providers := M{}
	ruleProv := M{}
	var groups []M
	var rules []string
	used := map[string]bool{}

	addRP := func(name, behavior, path string) {
		ruleProv[name] = M{"type": "file", "behavior": behavior, "format": "text", "path": path}
	}
	svcRules := func(ids []string, target string) {
		for _, id := range ids {
			if app.Exists(listFile(id)) || app.Dev {
				addRP(id, "domain", listFile(id))
				if c.BlockQUIC {
					rules = append(rules, fmt.Sprintf("AND,((NETWORK,UDP),(DST-PORT,443),(RULE-SET,%s)),REJECT", id))
				}
				rules = append(rules, "RULE-SET,"+id+","+target)
			}
			if s, _ := serviceByID(id); s.Subnets != "" && (app.Exists(subnetFile(id)) || app.Dev) {
				addRP(id+"_ip", "ipcidr", subnetFile(id))
				rules = append(rules, "RULE-SET,"+id+"_ip,"+target+",no-resolve")
			}
		}
	}
	custom := func(domains, subnets, target string) {
		for _, d := range splitList(domains) {
			rules = append(rules, "DOMAIN-SUFFIX,"+normDomain(d)+","+target)
		}
		for _, n := range splitList(subnets) {
			if !strings.Contains(n, "/") {
				if strings.Contains(n, ":") {
					n += "/128"
				} else {
					n += "/32"
				}
			}
			kind := "IP-CIDR"
			if strings.Contains(n, ":") {
				kind = "IP-CIDR6"
			}
			rules = append(rules, kind+","+n+","+target+",no-resolve")
		}
	}

	// exclusions first: always direct
	exD, exN := splitDomainsCIDR(c.Exclude)
	custom(exD, exN, "DIRECT")
	rules = append(rules, "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
		"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,169.254.0.0/16,DIRECT,no-resolve")

	needWarp := c.Steer.Enabled
	for _, s := range c.Sections {
		if s.Enabled && s.Mode == "warp" {
			needWarp = true
		}
	}
	if needWarp {
		wp := warpProxies(c)
		if len(wp) == 0 {
			return nil, fmt.Errorf("нет ключей WARP — нажмите «Подобрать туннели WARP» на странице Steer")
		}
		var names []string
		for _, p := range wp {
			proxies = append(proxies, p)
			names = append(names, p["name"].(string))
		}
		groups = append(groups, M{"name": "WARP", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204",
			"interval": 300, "tolerance": 80, "lazy": false})
		used["WARP"] = true
	}

	for _, s := range c.Sections {
		if !s.Enabled {
			continue
		}
		g := groupName(s, used)
		var use []string
		switch s.Mode {
		case "links":
			p := filepath.Join(provDir(), s.ID+"-links.txt")
			_ = os.MkdirAll(provDir(), 0o755)
			_ = app.WriteText(p, strings.Join(splitLines(s.Links), "\n")+"\n")
			providers[s.ID+"-links"] = M{"type": "file", "path": p,
				"health-check": M{"enable": true, "url": "https://www.gstatic.com/generate_204", "interval": 300}}
			use = append(use, s.ID+"-links")
		case "subscription":
			iv := map[string]int{"30m": 1800, "1h": 3600, "3h": 10800, "6h": 21600, "12h": 43200, "1d": 86400}[s.SubIv]
			if iv == 0 {
				iv = 21600
			}
			for i, u := range splitList(s.SubURLs) {
				pv := M{"type": "http", "url": u, "interval": iv, "path": filepath.Join(provDir(), fmt.Sprintf("%s-sub%d.yaml", s.ID, i)),
					"health-check": M{"enable": true, "url": "https://www.gstatic.com/generate_204", "interval": 300},
					"header":       M{"User-Agent": []string{"clash.meta"}}}
				if f := strings.TrimSpace(s.Filter); f != "" {
					pv["filter"] = kwRegex(f)
				}
				if f := strings.TrimSpace(s.ExFilter); f != "" {
					pv["exclude-filter"] = kwRegex(f)
				}
				providers[fmt.Sprintf("%s-sub%d", s.ID, i)] = pv
				use = append(use, fmt.Sprintf("%s-sub%d", s.ID, i))
			}
		case "interface":
			name := s.ID + " → " + s.Iface
			proxies = append(proxies, M{"name": name, "type": "direct", "interface-name": s.Iface, "udp": true})
			groups = append(groups, M{"name": g, "type": "select", "proxies": []string{name}})
		case "warp":
			groups = append(groups, M{"name": g, "type": "select", "proxies": []string{"WARP"}})
		}
		if len(use) > 0 {
			auto := g + " · авто"
			groups = append(groups,
				M{"name": g, "type": "select", "proxies": []string{auto}, "use": use},
				M{"name": auto, "type": "url-test", "use": use, "url": "https://www.gstatic.com/generate_204", "interval": 300, "tolerance": 50})
		}
		svcRules(s.Services, g)
		custom(s.Domains, s.Subnets, g)
		for i := range splitList(s.ListURLs) {
			base := filepath.Join(rulesDir(), fmt.Sprintf("%s_ext%d.txt", s.ID, i))
			if app.Exists(base) {
				addRP(fmt.Sprintf("%s_ext%d", s.ID, i), "domain", base)
				rules = append(rules, fmt.Sprintf("RULE-SET,%s_ext%d,%s", s.ID, i, g))
			} else if app.Exists(base + ".ip") {
				addRP(fmt.Sprintf("%s_ext%d_ip", s.ID, i), "ipcidr", base+".ip")
				rules = append(rules, fmt.Sprintf("RULE-SET,%s_ext%d_ip,%s,no-resolve", s.ID, i, g))
			}
		}
	}
	if c.Steer.Enabled {
		svcRules(c.Steer.Services, "WARP")
		custom(c.Steer.Domains, "", "WARP")
	}
	rules = append(rules, "MATCH,DIRECT")
	if len(proxies) > 0 {
		m["proxies"] = proxies
	}
	if len(providers) > 0 {
		m["proxy-providers"] = providers
	}
	if len(groups) > 0 {
		m["proxy-groups"] = groups
	}
	if len(ruleProv) > 0 {
		m["rule-providers"] = ruleProv
	}
	m["rules"] = rules
	return m, nil
}

func kwRegex(s string) string {
	var parts []string
	for _, k := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '|' || r == '\n' }) {
		if k = strings.TrimSpace(k); k != "" {
			parts = append(parts, regexp.QuoteMeta(k))
		}
	}
	return "(?i)" + strings.Join(parts, "|")
}

// MixomoTemplate builds a ready config from a subscription (router: Mixomo install).
func MixomoTemplate(c Config) M {
	m := baseConfig(c)
	var groups []M
	var rules []string
	ruleProv := M{}
	main := []string{"PROXY · авто"}
	if c.Mixomo.SubURL != "" {
		pv := M{"type": "http", "url": c.Mixomo.SubURL, "interval": 21600, "path": filepath.Join(provDir(), "mixomo-sub.yaml"),
			"health-check": M{"enable": true, "url": "https://www.gstatic.com/generate_204", "interval": 300}}
		if f := strings.TrimSpace(c.Mixomo.Filter); f != "" {
			pv["exclude-filter"] = kwRegex(f)
		}
		m["proxy-providers"] = M{"sub": pv}
	}
	if c.Mixomo.WarpProxy {
		if wp := warpProxies(c); len(wp) > 0 {
			m["proxies"] = wp
			var names []string
			for _, p := range wp {
				names = append(names, p["name"].(string))
			}
			groups = append(groups, M{"name": "WARP", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 300})
			main = append(main, "WARP")
		}
	}
	auto := M{"name": "PROXY · авто", "type": "url-test", "url": "https://www.gstatic.com/generate_204", "interval": 300, "tolerance": 50}
	sel := M{"name": "PROXY", "type": "select", "proxies": append(main, "DIRECT")}
	if c.Mixomo.SubURL != "" {
		auto["use"] = []string{"sub"}
		sel["use"] = []string{"sub"}
	} else {
		auto["proxies"] = []string{"DIRECT"}
	}
	groups = append([]M{sel, auto}, groups...)
	preset := c.Mixomo.ListPreset
	if preset == "" {
		preset = "russia_inside"
	}
	for _, id := range strings.Split(preset, ",") {
		if _, ok := serviceByID(id); ok {
			ruleProv[id] = M{"type": "file", "behavior": "domain", "format": "text", "path": listFile(id)}
			rules = append(rules, "RULE-SET,"+id+",PROXY")
			if s, _ := serviceByID(id); s.Subnets != "" {
				ruleProv[id+"_ip"] = M{"type": "file", "behavior": "ipcidr", "format": "text", "path": subnetFile(id)}
				rules = append(rules, "RULE-SET,"+id+"_ip,PROXY,no-resolve")
			}
		}
	}
	rules = append(rules, "MATCH,DIRECT")
	m["proxy-groups"] = groups
	m["rule-providers"] = ruleProv
	m["rules"] = rules
	return m
}

// ---------------- run ----------------

type Mode string

func (c Config) mode() string {
	if c.Mixomo.Enabled {
		return "mixomo"
	}
	if c.Steer.Enabled {
		return "generated"
	}
	for _, s := range c.Sections {
		if s.Enabled {
			return "generated"
		}
	}
	return "off"
}

// Restart regenerates the config and (re)starts mihomo, or stops it when nothing is enabled.
func Restart() error {
	mu.Lock()
	c := Load()
	mu.Unlock()
	mode := c.mode()
	if mode == "off" || !Installed() {
		osx.ProcStop(procName)
		return nil
	}
	conf := genConf()
	if mode == "generated" {
		m, err := Generate(c)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(m, "", "  ")
		if err := app.WriteFileAtomic(conf, b); err != nil {
			return err
		}
	} else {
		conf = MixomoConf()
		if !app.Exists(conf) {
			return fmt.Errorf("нет конфигурации Mixomo — вставьте свою или создайте по подписке")
		}
	}
	args := []string{"-d", Dir(), "-f", conf, "-ext-ctl", fmt.Sprintf("127.0.0.1:%d", c.CtrlPort), "-secret", c.Secret}
	if UIInstalled() {
		args = append(args, "-ext-ui", uiDir())
	}
	if !app.Dev {
		if out, err := osx.Run(nil, 30*time.Second, exe(), "-t", "-d", Dir(), "-f", conf); err != nil {
			return fmt.Errorf("mihomo отклонил конфигурацию: %s", lastLines(out, 4))
		}
	}
	if err := osx.ProcStart(osx.ProcSpec{Name: procName, Exe: exe(), Args: args, Dir: Dir(), Restart: true}); err != nil {
		return err
	}
	go applyPins(c)
	return nil
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, " / ")
}

func Stop() { osx.ProcStop(procName) }

func Boot() {
	if Installed() && Load().mode() != "off" {
		if err := Restart(); err != nil {
			app.Logf("routing: %v", err)
		}
	}
}

// ---------------- controller API ----------------

func api(method, path string, body any, out any) error {
	c := Load()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", c.CtrlPort, path), rd)
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return fmt.Errorf("mihomo не отвечает: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mihomo: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type Node struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Delay int    `json:"delay"`
	Alive bool   `json:"alive"`
}

type Group struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Now   string `json:"now"`
	Nodes []Node `json:"nodes"`
}

// Groups lists selectable groups with their nodes and last delays (mixomo_proxies / forkop servers).
func Groups() ([]Group, error) {
	var r struct {
		Proxies map[string]struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Now     string   `json:"now"`
			All     []string `json:"all"`
			Alive   bool     `json:"alive"`
			History []struct {
				Delay int `json:"delay"`
			} `json:"history"`
		} `json:"proxies"`
	}
	if err := api("GET", "/proxies", nil, &r); err != nil {
		return nil, err
	}
	var out []Group
	for _, p := range r.Proxies {
		if p.Type != "Selector" && p.Type != "URLTest" && p.Type != "Fallback" {
			continue
		}
		if p.Name == "GLOBAL" {
			continue
		}
		g := Group{Name: p.Name, Type: p.Type, Now: p.Now}
		for _, n := range p.All {
			x := r.Proxies[n]
			nd := Node{Name: n, Type: x.Type, Alive: x.Alive}
			if len(x.History) > 0 {
				nd.Delay = x.History[len(x.History)-1].Delay
			}
			g.Nodes = append(g.Nodes, nd)
		}
		out = append(out, g)
	}
	sortGroups(out)
	return out, nil
}

func sortGroups(g []Group) {
	for i := 1; i < len(g); i++ {
		for k := i; k > 0 && g[k].Name < g[k-1].Name; k-- {
			g[k], g[k-1] = g[k-1], g[k]
		}
	}
}

func Select(group, node string) error {
	return api("PUT", "/proxies/"+url.PathEscape(group), M{"name": node}, nil)
}

func TestDelay(group string) (map[string]int, error) {
	out := map[string]int{}
	err := api("GET", "/group/"+url.PathEscape(group)+"/delay?url="+url.QueryEscape("https://www.gstatic.com/generate_204")+"&timeout=4000", nil, &out)
	return out, err
}

func RefreshProviders() error {
	var r struct {
		Providers map[string]struct {
			VehicleType string `json:"vehicleType"`
		} `json:"providers"`
	}
	if err := api("GET", "/providers/proxies", nil, &r); err != nil {
		return err
	}
	for name, p := range r.Providers {
		if p.VehicleType == "HTTP" {
			_ = api("PUT", "/providers/proxies/"+url.PathEscape(name), nil, nil)
		}
	}
	return nil
}

// applyPins restores user-pinned servers after a restart.
func applyPins(c Config) {
	time.Sleep(3 * time.Second)
	used := map[string]bool{}
	if c.Steer.Enabled {
		used["WARP"] = true
	}
	for _, s := range c.Sections {
		if !s.Enabled {
			continue
		}
		g := groupName(s, used)
		if s.Pinned != "" {
			_ = Select(g, s.Pinned)
		}
	}
}

func Traffic() (up, down int64) {
	var r struct {
		Up   int64 `json:"uploadTotal"`
		Down int64 `json:"downloadTotal"`
	}
	if api("GET", "/connections", nil, &r) == nil {
		return r.Up, r.Down
	}
	return 0, 0
}

// splitDomainsCIDR separates a mixed list into domains and addresses/subnets.
func splitDomainsCIDR(s string) (domains, nets string) {
	var d, n []string
	for _, x := range splitList(s) {
		if ip := net.ParseIP(strings.Split(x, "/")[0]); ip != nil {
			n = append(n, x)
		} else {
			d = append(d, x)
		}
	}
	return strings.Join(d, "\n"), strings.Join(n, "\n")
}
