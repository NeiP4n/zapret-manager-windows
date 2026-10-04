//go:build !windows

package osx

import (
	"os/exec"
	"strings"
	"syscall"

	"github.com/zapretmanager/zmwin/internal/app"
)

// Dev-mode stand-ins. Read-only network tools run for real so tests/pings still work.
func devAllowed(name string) bool {
	switch name {
	case "ping", "curl", "nslookup":
		return true
	}
	return false
}

func devRun(name string, args []string) string {
	if name == "tasklist" {
		return ""
	}
	return ""
}

func devPS(script string) string {
	switch {
	case strings.Contains(script, "Get-ComputerInfo"), strings.Contains(script, "CurrentBuild"):
		return "Windows 11 Pro (dev)|26100"
	}
	return ""
}

func hideWindow(cmd *exec.Cmd) {}

func attachKillOnClose(cmd *exec.Cmd) {}

func killTree(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

func IsAdmin() bool { return true }

func Elevate(args []string) error { return nil }

func OpenURL(u string) { app.Logf("[dev] open %s", u) }

func Processes() map[string][]int { return map[string][]int{} }

var devSvc = false

func ServiceInstalled() bool          { return devSvc }
func ServiceRunning() bool            { return devSvc }
func ServiceInstall(exe string) error { devSvc = true; return nil }
func ServiceStart() error             { return nil }
func ServiceStop() error              { return nil }
func ServiceRemove() error            { devSvc = false; return nil }
func ServiceState(name string) string { return "" }
func UnloadWinDivert()                {}

func ServiceDisable(name string) error { return nil }
func KillPID(pid int)                  {}
