// Package sysinfo is the "Система" page: machine info, connectivity, QUIC block, IPv6, time,
// GitHub mirror and the manager's own maintenance.
package sysinfo

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/zapret"
)

type Info struct {
	Host     string   `json:"host"`
	OS       string   `json:"os"`
	Build    string   `json:"build"`
	Uptime   int64    `json:"uptime"`
	MemTotal uint64   `json:"mem_total"`
	MemUsed  uint64   `json:"mem_used"`
	CPU      int      `json:"cpu"`
	IPs      []string `json:"ips"`
	Admin    bool     `json:"admin"`
	Version  string   `json:"version"`
	Base     string   `json:"base"`
	Dev      bool     `json:"dev"`
	Now      int64    `json:"now"`
}

var osName, osBuild string

func Get() Info {
	in := Info{Version: app.Version, Base: app.Base, Dev: app.Dev, Admin: osx.IsAdmin(), Now: time.Now().Unix()}
	in.Host, _ = os.Hostname()
	if osName == "" {
		out, _ := osx.PS(nil, 15*time.Second, `$v=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'; $n=$v.ProductName; if([int]$v.CurrentBuild -ge 22000){$n=$n -replace 'Windows 10','Windows 11'}; $n+'|'+$v.DisplayVersion+' ('+$v.CurrentBuild+'.'+$v.UBR+')'`)
		if p := strings.SplitN(strings.TrimSpace(out), "|", 2); len(p) == 2 {
			osName, osBuild = p[0], p[1]
		}
		if app.Dev {
			osName, osBuild = "Windows 11 Pro (dev)", "24H2 (26100)"
		}
	}
	in.OS, in.Build = osName, osBuild
	in.Uptime = uptime()
	in.MemTotal, in.MemUsed = memory()
	in.CPU = cpuLoad()
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
				in.IPs = append(in.IPs, ipn.IP.String())
			}
		}
	}
	return in
}

// LanIP returns the primary IPv4 address (for tg:// links, PAC on LAN etc.).
func LanIP() string {
	c, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

// ---------------- connectivity ----------------

type Conn struct {
	V4OK bool   `json:"ipv4_ok"`
	V4ms string `json:"ipv4_ms"`
	V6OK bool   `json:"ipv6_ok"`
	V6ms string `json:"ipv6_ms"`
}

func tcpTime(network string) (bool, string) {
	t0 := time.Now()
	c, err := net.DialTimeout(network, "google.com:443", 3*time.Second)
	if err != nil {
		return false, ""
	}
	c.Close()
	return true, fmt.Sprintf("%.0f", float64(time.Since(t0).Microseconds())/1000)
}

func Connectivity() Conn {
	var c Conn
	c.V4OK, c.V4ms = tcpTime("tcp4")
	c.V6OK, c.V6ms = tcpTime("tcp6")
	return c
}

// InternetOK is a quick reachability probe for the dashboard.
func InternetOK() bool {
	ok, _ := tcpTime("tcp4")
	return ok
}

// ---------------- QUIC / IPv6 / expert ----------------

const fwQuic443, fwQuic80 = "ZM Block QUIC UDP443", "ZM Block UDP80"

type Status struct {
	QuicBlocked bool   `json:"quic_blocked"`
	IPv6        bool   `json:"ipv6_enabled"`
	Expert      bool   `json:"expert_mode"`
	Mirror      string `json:"mirror"`
	Mirrors     any    `json:"mirrors"`
	Service     bool   `json:"service"`
	Port        int    `json:"port"`
	Theme       string `json:"theme"`
	Time        string `json:"time"`
}

func GetStatus() Status {
	s := app.S()
	m := "direct"
	for _, x := range app.Mirrors {
		if x.Prefix == s.Mirror {
			m = x.ID
		}
	}
	return Status{QuicBlocked: osx.FirewallRuleExists(fwQuic443), IPv6: !s.DisableIPv6, Expert: s.Expert, Mirror: m,
		Mirrors: app.Mirrors, Service: osx.ServiceInstalled(), Port: s.Port, Theme: s.Theme,
		Time: time.Now().Format("02.01.2006 15:04:05 MST")}
}

// ToggleQuic blocks/unblocks outbound UDP 443/80 so browsers fall back from QUIC to TCP
// (system_toggle_quic: Block_UDP_80 / Block_UDP_443 firewall rules).
func ToggleQuic() (bool, error) {
	if osx.FirewallRuleExists(fwQuic443) {
		osx.FirewallRemove(fwQuic443)
		osx.FirewallRemove(fwQuic80)
		return false, nil
	}
	if err := osx.FirewallBlock(fwQuic443, "UDP", "443"); err != nil {
		return false, err
	}
	_ = osx.FirewallBlock(fwQuic80, "UDP", "80")
	return true, nil
}

func ToggleIPv6() (bool, error) {
	s := app.S()
	if s.DisableIPv6 {
		if ok, _ := tcpTime("tcp6"); !ok {
			return false, fmt.Errorf("IPv6 на этом компьютере недоступен — включение не рекомендуется")
		}
	}
	s = app.Update(func(st *app.Settings) { st.DisableIPv6 = !st.DisableIPv6 })
	_ = zapret.Restart()
	_ = zapret.Restart2()
	return !s.DisableIPv6, nil
}

func ToggleExpert() bool {
	s := app.Update(func(st *app.Settings) { st.Expert = !st.Expert })
	return s.Expert
}

func SetMirror(id string) error {
	for _, m := range app.Mirrors {
		if m.ID == id {
			app.Update(func(s *app.Settings) { s.Mirror = m.Prefix })
			if m.Prefix != "" {
				if _, err := app.Fetch(context.Background(), "https://raw.githubusercontent.com/StressOzz/Zapret-Manager/refs/heads/main/README.md", 15*time.Second); err != nil {
					app.Update(func(s *app.Settings) { s.Mirror = "" })
					return fmt.Errorf("зеркало недоступно — возвращаемся к прямому доступу")
				}
			}
			return nil
		}
	}
	return fmt.Errorf("неизвестное зеркало")
}

func SetTheme(t string) {
	switch t {
	case "light", "dark", "auto", "ultrakill":
		app.Update(func(s *app.Settings) { s.Theme = t })
	}
}

// SyncTime asks Windows Time to resync (the router panel had to fix time manually — Windows has
// an RTC, but a wrong clock still breaks TLS).
func SyncTime() (string, error) {
	out, err := osx.Run(nil, 30*time.Second, "w32tm", "/resync", "/force")
	if err != nil {
		_, _ = osx.Run(nil, 20*time.Second, "net", "start", "w32time")
		out, err = osx.Run(nil, 30*time.Second, "w32tm", "/resync", "/force")
	}
	return out, err
}
