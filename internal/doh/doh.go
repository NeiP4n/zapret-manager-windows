// Package doh is the "DNS over HTTPS" page: provider choice, bootstrap DNS, forced DNS, and the
// switch of Windows network adapters to the local DoH proxy (or to native Windows 11 DoH).
package doh

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
)

type Provider struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Bootstrap string `json:"bootstrap,omitempty"`
}

// Providers — same set as the router panel (doh_set).
var Providers = []Provider{
	{"cloudflare", "Cloudflare", "https://cloudflare-dns.com/dns-query", "1.1.1.1,1.0.0.1,2606:4700:4700::1111,2606:4700:4700::1001"},
	{"google", "Google", "https://dns.google/dns-query", "8.8.8.8,8.8.4.4,2001:4860:4860::8888,2001:4860:4860::8844"},
	{"quad9", "Quad9", "https://dns.quad9.net/dns-query", "9.9.9.9,149.112.112.112,2620:fe::fe,2620:fe::9"},
	{"xbox", "Xbox DNS", "https://xbox-dns.ru/dns-query", ""},
	{"comss", "Comss.one DNS", "https://dns.comss.one/dns-query", ""},
	{"dnsai", "DNS-AI", "https://dns.dns-ai.ru/dns-query", ""},
	{"yandex_family", "Яндекс Семейный", "https://family.dot.dns.yandex.net/dns-query", "77.88.8.7,77.88.8.3"},
	{"yandex_safe", "Яндекс Безопасный", "https://safe.dot.dns.yandex.net/dns-query", "77.88.8.88,77.88.8.2"},
	{"geohide_ru", "GeoHide DNS (RU)", "https://geohide.ru/dns-query", ""},
	{"geohide_eu", "GeoHide DNS (EU)", "https://eu.geohide.ru/dns-query", ""},
	{"geohide_us", "GeoHide DNS (US)", "https://us.geohide.ru/dns-query", ""},
}

// default bootstrap for providers without their own (https-dns-proxy default)
var defaultBoot = []string{"77.88.8.8", "1.1.1.1", "8.8.8.8"}

type Adapter struct {
	Idx     int    `json:"idx"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	GUID    string `json:"guid"`
	Static4 string `json:"static4"`
	Static6 string `json:"static6"`
	Current string `json:"current,omitempty"`
}

type Config struct {
	Enabled   bool      `json:"enabled"`
	Provider  string    `json:"provider"`
	CustomURL string    `json:"custom_url"`
	Mode      string    `json:"mode"` // proxy | windows
	Force     bool      `json:"force"`
	Bootstrap string    `json:"bootstrap"`
	Saved     []Adapter `json:"saved"`
	NativeIPs []string  `json:"native_ips"`
}

var (
	kv    = app.NewKV("doh")
	mu    sync.Mutex
	proxy *Proxy
	perr  string
)

func load() Config {
	c := Config{Mode: "proxy", Provider: "cloudflare"}
	kv.Load(&c)
	if c.Mode == "" {
		c.Mode = "proxy"
	}
	return c
}

func save(c Config) { _ = kv.Save(&c) }

func providerByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

func (c Config) url() string {
	if c.Provider == "custom" {
		return c.CustomURL
	}
	p, _ := providerByID(c.Provider)
	return p.URL
}

func (c Config) bootstrap() []string {
	src := c.Bootstrap
	if src == "" {
		if p, ok := providerByID(c.Provider); ok {
			src = p.Bootstrap
		}
	}
	var out []string
	for _, ip := range strings.FieldsFunc(src, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if pi := net.ParseIP(ip); pi != nil && pi.To4() != nil {
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		out = defaultBoot
	}
	return out
}

const fwForceU, fwForceT = "ZM DoH Force UDP53", "ZM DoH Force TCP53"

type StatusInfo struct {
	Enabled    bool       `json:"installed"`
	Provider   string     `json:"current"`
	URL        string     `json:"url"`
	CustomURL  string     `json:"custom_url"`
	Mode       string     `json:"mode"`
	Running    bool       `json:"running"`
	Force      bool       `json:"force_dns"`
	Bootstrap  string     `json:"bootstrap_custom"`
	BootEff    []string   `json:"bootstrap"`
	Providers  []Provider `json:"providers"`
	Adapters   []Adapter  `json:"adapters"`
	Stats      Stats      `json:"stats"`
	Error      string     `json:"error,omitempty"`
	Win11      bool       `json:"win11"`
	HostsExtra bool       `json:"hosts_extra"`
}

func Status(withAdapters bool) StatusInfo {
	mu.Lock()
	c := load()
	st := StatusInfo{Enabled: c.Enabled, Provider: c.Provider, URL: c.url(), CustomURL: c.CustomURL, Mode: c.Mode,
		Force: c.Force, Bootstrap: c.Bootstrap, BootEff: c.bootstrap(), Providers: Providers, Error: perr, Win11: win11()}
	if proxy != nil {
		st.Running = true
		st.Stats = proxy.Stats()
	}
	if c.Enabled && c.Mode == "windows" {
		st.Running = true
	}
	mu.Unlock()
	if withAdapters {
		st.Adapters, _ = adapters()
	}
	return st
}

var (
	buildOnce sync.Once
	build     int
)

func win11() bool {
	buildOnce.Do(func() {
		out, _ := osx.PS(nil, 15*time.Second, "(Get-ItemProperty 'HKLM:\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion').CurrentBuild")
		fmt.Sscanf(strings.TrimSpace(out), "%d", &build)
		if app.Dev {
			build = 26100
		}
	})
	return build >= 22000
}

// ---------------- adapters ----------------

var skipAdapterRx = regexp.MustCompile(`(?i)wintun|meta|mihomo|amneziawg|wireguard|tap-windows|sing-tun|loopback|zapretmanager`)

func adapters() ([]Adapter, error) {
	if app.Dev {
		return []Adapter{{Idx: 12, Name: "Ethernet", Desc: "Intel(R) Ethernet (dev)", Current: "192.168.1.1"}}, nil
	}
	out, err := osx.PS(nil, 40*time.Second, `$r=@(); Get-NetAdapter | ?{$_.Status -eq 'Up'} | %{ $g=$_.InterfaceGuid; `+
		`$p4=(Get-ItemProperty ('HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\'+$g) -EA SilentlyContinue).NameServer; `+
		`$p6=(Get-ItemProperty ('HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters\Interfaces\'+$g) -EA SilentlyContinue).NameServer; `+
		`$cur=((Get-DnsClientServerAddress -InterfaceIndex $_.ifIndex -EA SilentlyContinue | %{$_.ServerAddresses}) -join ','); `+
		`$r+=[pscustomobject]@{idx=$_.ifIndex;name=$_.Name;desc=$_.InterfaceDescription;guid=$g;static4=[string]$p4;static6=[string]$p6;current=$cur} }; `+
		`ConvertTo-Json -InputObject $r -Compress`)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать сетевые адаптеры: %v %s", err, out)
	}
	var list []Adapter
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &list); err != nil {
		var one Adapter
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &one) == nil {
			list = []Adapter{one}
		}
	}
	var res []Adapter
	for _, a := range list {
		if !skipAdapterRx.MatchString(a.Desc + " " + a.Name) {
			res = append(res, a)
		}
	}
	return res, nil
}

func setAdapterDNS(a Adapter, servers []string) error {
	q := make([]string, len(servers))
	for i, s := range servers {
		q[i] = osx.PSQuote(s)
	}
	_, err := osx.PS(nil, 30*time.Second, fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s)", a.Idx, strings.Join(q, ",")))
	return err
}

func restoreAdapter(a Adapter) {
	var static []string
	for _, s := range strings.FieldsFunc(a.Static4+","+a.Static6, func(r rune) bool { return r == ',' || r == ' ' }) {
		if s != "" && s != "127.0.0.1" && s != "::1" {
			static = append(static, s)
		}
	}
	script := fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses", a.Idx)
	if len(static) > 0 {
		q := make([]string, len(static))
		for i, s := range static {
			q[i] = osx.PSQuote(s)
		}
		script = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses @(%s)", a.Idx, strings.Join(q, ","))
	}
	_, _ = osx.PS(nil, 30*time.Second, script)
}

// applySystem points every active adapter at the target servers, remembering originals.
func applySystem(c *Config, servers []string) error {
	list, err := adapters()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, s := range c.Saved {
		known[s.GUID] = true
	}
	want := strings.Join(servers, ",")
	for _, a := range list {
		if !known[a.GUID] {
			c.Saved = append(c.Saved, Adapter{Idx: a.Idx, Name: a.Name, Desc: a.Desc, GUID: a.GUID, Static4: a.Static4, Static6: a.Static6})
			known[a.GUID] = true
		}
		if a.Current != want {
			if err := setAdapterDNS(a, servers); err != nil {
				app.Logf("doh: set dns %s: %v", a.Name, err)
			}
		}
	}
	osx.FlushDNS()
	return nil
}

func restoreSystem(c *Config) {
	cur, _ := adapters()
	idx := map[string]int{}
	for _, a := range cur {
		idx[a.GUID] = a.Idx
	}
	for _, s := range c.Saved {
		if i, ok := idx[s.GUID]; ok {
			s.Idx = i
		}
		restoreAdapter(s)
	}
	c.Saved = nil
	osx.FlushDNS()
}

// ---------------- native Windows 11 DoH ----------------

func nativeApply(c *Config) error {
	u, err := url.Parse(c.url())
	if err != nil {
		return err
	}
	ips := Bootstrap(context.Background(), u.Hostname(), c.bootstrap(), false)
	if p, ok := providerByID(c.Provider); ok && p.Bootstrap != "" {
		ips = nil
		for _, ip := range strings.Split(p.Bootstrap, ",") {
			if net.ParseIP(ip).To4() != nil {
				ips = append(ips, ip)
			}
		}
	}
	if len(ips) == 0 {
		return fmt.Errorf("не удалось узнать IP-адреса %s", u.Hostname())
	}
	if len(ips) > 2 {
		ips = ips[:2]
	}
	nativeRemove(c)
	for _, ip := range ips {
		if _, err := osx.Run(nil, 20*time.Second, "netsh", "dns", "add", "encryption", "server="+ip, "dohtemplate="+c.url(), "autoupgrade=yes", "udpfallback=no"); err != nil {
			return fmt.Errorf("netsh dns add encryption: %v", err)
		}
	}
	c.NativeIPs = ips
	if err := applySystem(c, ips); err != nil {
		return err
	}
	list, _ := adapters()
	for _, a := range list {
		for _, ip := range ips {
			_, _ = osx.PS(nil, 20*time.Second, fmt.Sprintf(`$k='HKLM:\System\CurrentControlSet\Services\Dnscache\InterfaceSpecificParameters\%s\DohInterfaceSettings\Doh\%s'; New-Item -Path $k -Force | Out-Null; New-ItemProperty -Path $k -Name DohFlags -Value 1 -PropertyType QWord -Force | Out-Null`, a.GUID, ip))
		}
	}
	osx.FlushDNS()
	return nil
}

func nativeRemove(c *Config) {
	for _, ip := range c.NativeIPs {
		_, _ = osx.Run(nil, 20*time.Second, "netsh", "dns", "delete", "encryption", "server="+ip)
	}
	for _, s := range c.Saved {
		for _, ip := range c.NativeIPs {
			_, _ = osx.PS(nil, 20*time.Second, fmt.Sprintf(`Remove-Item -Path 'HKLM:\System\CurrentControlSet\Services\Dnscache\InterfaceSpecificParameters\%s\DohInterfaceSettings\Doh\%s' -Recurse -Force -EA SilentlyContinue`, s.GUID, ip))
		}
	}
	c.NativeIPs = nil
}

// ---------------- lifecycle ----------------

func startProxyLocked(c Config) error {
	if proxy != nil {
		proxy.Stop()
		proxy = nil
	}
	p := &Proxy{URL: c.url(), Bootstrap: c.bootstrap(), NoPlain: c.Force}
	listen := []string{"127.0.0.1:53", "[::1]:53"}
	if app.Dev {
		listen = []string{"127.0.0.1:5353"}
	}
	if err := p.Start(listen); err != nil {
		perr = err.Error()
		return err
	}
	perr = ""
	proxy = p
	return nil
}

func applyForce(c Config) {
	if c.Enabled && c.Force {
		_ = osx.FirewallBlock(fwForceU, "UDP", "53")
		_ = osx.FirewallBlock(fwForceT, "TCP", "53")
	} else {
		osx.FirewallRemove(fwForceU)
		osx.FirewallRemove(fwForceT)
	}
}

// Apply brings the system to the saved configuration (called on change and at service start).
func Apply() error {
	mu.Lock()
	defer mu.Unlock()
	c := load()
	defer func() { save(c) }()
	if !c.Enabled {
		if proxy != nil {
			proxy.Stop()
			proxy = nil
		}
		nativeRemove(&c)
		restoreSystem(&c)
		applyForce(c)
		return nil
	}
	if c.url() == "" {
		return fmt.Errorf("не задан адрес DoH")
	}
	if c.Mode == "windows" {
		if proxy != nil {
			proxy.Stop()
			proxy = nil
		}
		if err := nativeApply(&c); err != nil {
			perr = err.Error()
			return err
		}
		perr = ""
	} else {
		nativeRemove(&c)
		if err := startProxyLocked(c); err != nil {
			return err
		}
		if err := applySystem(&c, []string{"127.0.0.1", "::1"}); err != nil {
			return err
		}
	}
	applyForce(c)
	return nil
}

// Shutdown restores adapters when the service stops, so the PC never stays without DNS.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	c := load()
	if !c.Enabled || c.Mode != "proxy" {
		return
	}
	if proxy != nil {
		proxy.Stop()
		proxy = nil
	}
	restoreSystem(&c)
	osx.FirewallRemove(fwForceU)
	osx.FirewallRemove(fwForceT)
	save(c)
}

// Watch re-applies DNS to adapters that appeared later (new Wi-Fi, docking station…).
func Watch() {
	mu.Lock()
	c := load()
	mu.Unlock()
	if !c.Enabled || c.Mode != "proxy" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	c = load()
	_ = applySystem(&c, []string{"127.0.0.1", "::1"})
	save(c)
}

func Enable(on bool) error {
	mu.Lock()
	c := load()
	c.Enabled = on
	save(c)
	mu.Unlock()
	return Apply()
}

func SetProvider(id, custom string) error {
	if id == "custom" {
		u, err := url.Parse(custom)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("адрес должен быть вида https://сервер/dns-query")
		}
	} else if _, ok := providerByID(id); !ok {
		return fmt.Errorf("неизвестный провайдер")
	}
	mu.Lock()
	c := load()
	c.Provider, c.CustomURL = id, custom
	c.Enabled = true
	save(c)
	mu.Unlock()
	return Apply()
}

func SetMode(mode string) error {
	if mode != "proxy" && mode != "windows" {
		return fmt.Errorf("неизвестный режим")
	}
	if mode == "windows" && !win11() {
		return fmt.Errorf("встроенный DoH есть только в Windows 11 — используйте режим «Свой DoH-прокси»")
	}
	mu.Lock()
	c := load()
	if c.Enabled && c.Mode != mode {
		if c.Mode == "windows" {
			nativeRemove(&c)
		} else if proxy != nil {
			proxy.Stop()
			proxy = nil
		}
		restoreSystem(&c)
	}
	c.Mode = mode
	save(c)
	mu.Unlock()
	return Apply()
}

func SetForce(on bool) error {
	mu.Lock()
	c := load()
	c.Force = on
	save(c)
	mu.Unlock()
	return Apply()
}

// SetBootstrap validates "IP, IP" (≤6) like doh_bootstrap_set.
func SetBootstrap(v string) error {
	var out []string
	seen := map[string]bool{}
	for _, ip := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("«%s» — не IP-адрес. Укажите IP через запятую, например 77.88.8.8, 1.1.1.1", ip)
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	if len(out) > 6 {
		return fmt.Errorf("укажите не больше 6 адресов")
	}
	mu.Lock()
	c := load()
	c.Bootstrap = strings.Join(out, ",")
	save(c)
	en := c.Enabled
	mu.Unlock()
	if en {
		return Apply()
	}
	return nil
}

// Test resolves a name through the configured (or given) provider.
func Test(ctx context.Context, provider, name string) ([]string, time.Duration, error) {
	c := load()
	if provider != "" {
		c.Provider = provider
	}
	if name == "" {
		name = "youtube.com"
	}
	t0 := time.Now()
	ips, err := Query(ctx, c.url(), name, c.bootstrap())
	return ips, time.Since(t0), err
}
