// Package osx is the Windows system layer: hidden command execution, PowerShell, firewall,
// processes, elevation and the service. Non-Windows builds get a simulated layer so the panel
// can be developed and tested anywhere (ZM dev mode).
package osx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
)

// Run executes a console program without a window and returns combined output.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if app.Dev && !devAllowed(name) {
		app.Logf("[dev] run: %s %s", name, strings.Join(args, " "))
		return devRun(name, args), nil
	}
	cmd := exec.CommandContext(c, name, args...)
	hideWindow(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(strings.ReplaceAll(buf.String(), "\r", ""))
	if err != nil && c.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s: превышено время ожидания", name)
	}
	return out, err
}

// PS runs a PowerShell script with UTF-8 output (avoids cp866/cp1251 mojibake on Russian Windows).
func PS(ctx context.Context, timeout time.Duration, script string) (string, error) {
	full := "$ProgressPreference='SilentlyContinue';[Console]::OutputEncoding=[Text.Encoding]::UTF8;" + script
	if app.Dev {
		app.Logf("[dev] ps: %s", script)
		return devPS(script), nil
	}
	return Run(ctx, timeout, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", full)
}

// PSQuote quotes a string for a single-quoted PowerShell literal.
func PSQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ---- firewall (Windows Defender Firewall rules created by the manager carry a "ZM " prefix) ----

func FirewallRuleExists(name string) bool {
	if app.Dev {
		return devFW[name]
	}
	out, _ := PS(nil, 20*time.Second, "if(Get-NetFirewallRule -DisplayName "+PSQuote(name)+" -ErrorAction SilentlyContinue){'yes'}")
	return strings.Contains(out, "yes")
}

// FirewallBlock adds an outbound block rule: proto TCP/UDP, ports like "443" or "80,443".
func FirewallBlock(name, proto, ports string) error {
	if app.Dev {
		devFW[name] = true
		return nil
	}
	p := "@(" + quoteList(strings.Split(ports, ",")) + ")"
	_, err := PS(nil, 30*time.Second, fmt.Sprintf(
		"Remove-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue; New-NetFirewallRule -DisplayName %s -Direction Outbound -Protocol %s -RemotePort %s -Action Block -Profile Any | Out-Null",
		PSQuote(name), PSQuote(name), proto, p))
	return err
}

// FirewallAllowInbound opens a listening port for LAN clients (TG proxy, etc.).
func FirewallAllowInbound(name, proto, port string) error {
	if app.Dev {
		devFW[name] = true
		return nil
	}
	_, err := PS(nil, 30*time.Second, fmt.Sprintf(
		"Remove-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue; New-NetFirewallRule -DisplayName %s -Direction Inbound -Protocol %s -LocalPort %s -Action Allow -Profile Private,Domain | Out-Null",
		PSQuote(name), PSQuote(name), proto, port))
	return err
}

func FirewallRemove(name string) {
	if app.Dev {
		delete(devFW, name)
		return
	}
	_, _ = PS(nil, 30*time.Second, "Remove-NetFirewallRule -DisplayName "+PSQuote(name)+" -ErrorAction SilentlyContinue")
}

func quoteList(xs []string) string {
	var q []string
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			q = append(q, PSQuote(x))
		}
	}
	return strings.Join(q, ",")
}

var devFW = map[string]bool{}

// FlushDNS clears the Windows resolver cache after hosts / DNS changes (router: dnsmasq restart).
func FlushDNS() { _, _ = Run(nil, 15*time.Second, "ipconfig", "/flushdns") }
