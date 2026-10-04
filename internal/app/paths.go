// Package app holds what every module shares: paths, persistent state, background jobs,
// downloads and logging. Mirrors the router's /opt/zapret + /opt/zapret-manager-luci layout.
package app

import (
	"os"
	"path/filepath"
	"runtime"
)

// Version is overridable at build time: -ldflags "-X github.com/zapretmanager/zmwin/internal/app.Version=1.2.3"
var Version = "1.1.0"

const (
	ServiceName = "ZapretManager"
	DisplayName = "Zapret Manager"
	DefaultPort = 17580
)

// Base is the data root: %ProgramData%\ZapretManager on Windows, ./devroot elsewhere (dev mode).
var Base = func() string {
	if v := os.Getenv("ZM_BASE"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "ZapretManager")
	}
	wd, _ := os.Getwd()
	return filepath.Join(wd, "devroot")
}()

// Dev is true when running outside Windows: system actions are simulated.
var Dev = runtime.GOOS != "windows"

func P(parts ...string) string { return filepath.Join(append([]string{Base}, parts...)...) }

var (
	BinZapret  = P("bin", "zapret")
	BinZapret2 = P("bin", "zapret2")
	FakeDir    = P("files", "fake")
	ListsDir   = P("lists")
	StateDir   = P("state")
	LogsDir    = P("logs")
	ToolsDir   = P("tools")
	TmpDir     = P("tmp")
)

// ExePath is where the installed copy of the manager lives (service binPath points here).
func ExePath() string { return P("ZapretManager.exe") }

func EnsureDirs() {
	for _, d := range []string{Base, BinZapret, BinZapret2, FakeDir, ListsDir, StateDir, LogsDir, ToolsDir, TmpDir} {
		_ = os.MkdirAll(d, 0o755)
	}
}

func Exists(p string) bool { _, err := os.Stat(p); return err == nil }

func ReadText(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// WriteFileAtomic writes via a temp file + rename so a crash never leaves a half-written config.
func WriteFileAtomic(p string, data []byte) error {
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".zmtmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func WriteText(p, s string) error { return WriteFileAtomic(p, []byte(s)) }

func Touch(p string) { _ = WriteText(p, "") }

func Remove(p ...string) {
	for _, x := range p {
		_ = os.RemoveAll(x)
	}
}
