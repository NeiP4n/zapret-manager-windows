//go:build windows

package osx

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/zapretmanager/zmwin/internal/app"
)

func devAllowed(string) bool         { return true }
func devRun(string, []string) string { return "" }
func devPS(string) string            { return "" }

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// One job object for every helper: if the manager dies, Windows kills winws/mihomo/etc. with it,
// so WinDivert never stays loaded by an orphan.
var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

func killJob() windows.Handle {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
	return jobHandle
}

func attachKillOnClose(cmd *exec.Cmd) {
	j := killJob()
	if j == 0 || cmd.Process == nil {
		return
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	_ = windows.AssignProcessToJobObject(j, ph)
	windows.CloseHandle(ph)
}

func killTree(pid int) {
	_, _ = Run(nil, 10*time.Second, "taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
}

// IsAdmin reports whether the process token is elevated.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// Elevate relaunches the current exe with "runas" (UAC prompt) and the given arguments.
func Elevate(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(joinArgs(args))
	cwd, _ := windows.UTF16PtrFromString(app.Base)
	return windows.ShellExecute(0, verb, file, params, cwd, windows.SW_NORMAL)
}

func joinArgs(args []string) string {
	var q []string
	for _, a := range args {
		q = append(q, syscall.EscapeArg(a))
	}
	return strings.Join(q, " ")
}

// OpenURL opens a link in the default browser.
func OpenURL(u string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(u)
	_ = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// Processes returns running process names (lowercase) → pids.
func Processes() map[string][]int {
	out := map[string][]int{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		n := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		out[n] = append(out[n], int(e.ProcessID))
	}
	return out
}

// ---- the manager's own Windows service ----

func ServiceInstalled() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(app.ServiceName)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

func ServiceRunning() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(app.ServiceName)
	if err != nil {
		return false
	}
	defer s.Close()
	st, err := s.Query()
	return err == nil && st.State == windows.SERVICE_RUNNING
}

func ServiceInstall(exe string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(app.ServiceName); err == nil {
		s.Close()
		return nil
	}
	s, err := m.CreateService(app.ServiceName, exe, mgr.Config{
		DisplayName:  app.DisplayName,
		Description:  "Обход DPI (zapret/winws), DoH, hosts, прокси и туннели — веб-панель на 127.0.0.1",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "service")
	if err != nil {
		return err
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	return nil
}

func ServiceStart() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(app.ServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Start()
}

func ServiceStop() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(app.ServiceName)
	if err != nil {
		return err
	}
	defer s.Close()
	st, err := s.Control(windows.SERVICE_CONTROL_STOP)
	if err != nil {
		return err
	}
	for i := 0; i < 60 && st.State != windows.SERVICE_STOPPED; i++ {
		time.Sleep(250 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			break
		}
	}
	return nil
}

func ServiceRemove() error {
	_ = ServiceStop()
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(app.ServiceName)
	if err != nil {
		return nil
	}
	defer s.Close()
	return s.Delete()
}

// ServiceState reports another (foreign) service's state, e.g. Flowseal's "zapret" or "WinDivert".
func ServiceState(name string) string {
	m, err := mgr.Connect()
	if err != nil {
		return ""
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return ""
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "unknown"
	}
	switch st.State {
	case windows.SERVICE_RUNNING:
		return "running"
	case windows.SERVICE_STOPPED:
		return "stopped"
	}
	return fmt.Sprint(st.State)
}

// UnloadWinDivert stops the kernel driver service after all users exit, so a new version can load.
func UnloadWinDivert() {
	for _, n := range []string{"WinDivert", "WinDivert14"} {
		_, _ = Run(nil, 15*time.Second, "sc.exe", "stop", n)
	}
}

// ServiceDisable stops a foreign service and switches it to manual start (reversible).
func ServiceDisable(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != windows.SERVICE_STOPPED {
		_, _ = s.Control(windows.SERVICE_CONTROL_STOP)
		for i := 0; i < 40; i++ {
			time.Sleep(250 * time.Millisecond)
			if st, err = s.Query(); err != nil || st.State == windows.SERVICE_STOPPED {
				break
			}
		}
	}
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	cfg.StartType = mgr.StartManual
	return s.UpdateConfig(cfg)
}

// KillPID terminates a foreign process tree.
func KillPID(pid int) { killTree(pid) }
