package osx

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
)

// ProcSpec describes a helper process the manager supervises (winws, winws2, ciadpi, mihomo,
// tg-ws-proxy). On the router these are procd services; here the manager service is procd.
type ProcSpec struct {
	Name    string
	Exe     string
	Args    []string
	Dir     string
	Restart bool
}

type proc struct {
	spec     ProcSpec
	cmd      *exec.Cmd
	running  bool
	stopping bool
	started  time.Time
	restarts int
	lastErr  string
	pid      int
	stopCh   chan struct{}
}

var (
	procMu sync.Mutex
	procs  = map[string]*proc{}
)

func LogPath(name string) string { return app.P("logs", name+".log") }

// ProcStart (re)starts a supervised process.
func ProcStart(spec ProcSpec) error {
	ProcStop(spec.Name)
	if !app.Exists(spec.Exe) && !app.Dev {
		return fmt.Errorf("не найден %s — переустановите компонент", filepath.Base(spec.Exe))
	}
	p := &proc{spec: spec, stopCh: make(chan struct{})}
	procMu.Lock()
	procs[spec.Name] = p
	procMu.Unlock()
	rotateLog(LogPath(spec.Name))
	if err := p.launch(); err != nil {
		procMu.Lock()
		delete(procs, spec.Name)
		procMu.Unlock()
		return err
	}
	go p.watch()
	return nil
}

func (p *proc) launch() error {
	app.Logf("start %s: %s %s", p.spec.Name, p.spec.Exe, strings.Join(p.spec.Args, " "))
	if app.Dev && (!app.Exists(p.spec.Exe) || strings.HasSuffix(strings.ToLower(p.spec.Exe), ".exe")) {
		procMu.Lock()
		p.running, p.started, p.pid = true, time.Now(), 4242
		procMu.Unlock()
		appendLog(p.spec.Name, "[dev] simulated: "+p.spec.Exe+" "+strings.Join(p.spec.Args, " "))
		return nil
	}
	cmd := exec.Command(p.spec.Exe, p.spec.Args...)
	cmd.Dir = p.spec.Dir
	hideWindow(cmd)
	lf, err := os.OpenFile(LogPath(p.spec.Name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout, cmd.Stderr = lf, lf
	} else {
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	}
	if err := cmd.Start(); err != nil {
		if lf != nil {
			lf.Close()
		}
		return err
	}
	attachKillOnClose(cmd)
	procMu.Lock()
	p.cmd, p.running, p.started, p.pid = cmd, true, time.Now(), cmd.Process.Pid
	procMu.Unlock()
	go func() {
		err := cmd.Wait()
		if lf != nil {
			lf.Close()
		}
		procMu.Lock()
		p.running = false
		if err != nil && !p.stopping {
			p.lastErr = err.Error()
		}
		procMu.Unlock()
	}()
	return nil
}

// watch restarts a crashed process with growing back-off.
func (p *proc) watch() {
	backoff := 2 * time.Second
	for {
		select {
		case <-p.stopCh:
			return
		case <-time.After(time.Second):
		}
		procMu.Lock()
		dead := !p.running && !p.stopping
		quick := time.Since(p.started) < 5*time.Second
		procMu.Unlock()
		if !dead {
			if !quick {
				backoff = 2 * time.Second
			}
			continue
		}
		tail := TailLog(p.spec.Name, 3)
		app.Logf("%s exited: %s | %s", p.spec.Name, p.lastErr, strings.Join(tail, " / "))
		if !p.spec.Restart {
			return
		}
		select {
		case <-p.stopCh:
			return
		case <-time.After(backoff):
		}
		if backoff < 60*time.Second {
			backoff *= 2
		}
		procMu.Lock()
		p.restarts++
		procMu.Unlock()
		if err := p.launch(); err != nil {
			procMu.Lock()
			p.lastErr = err.Error()
			procMu.Unlock()
		}
	}
}

func ProcStop(name string) {
	procMu.Lock()
	p := procs[name]
	if p == nil {
		procMu.Unlock()
		return
	}
	p.stopping = true
	close(p.stopCh)
	cmd := p.cmd
	delete(procs, name)
	procMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		killTree(cmd.Process.Pid)
		_ = cmd.Process.Kill()
		for i := 0; i < 30; i++ {
			procMu.Lock()
			r := p.running
			procMu.Unlock()
			if !r {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	app.Logf("stop %s", name)
}

type ProcState struct {
	Running  bool   `json:"running"`
	PID      int    `json:"pid"`
	Uptime   int64  `json:"uptime"`
	Restarts int    `json:"restarts"`
	LastErr  string `json:"last_error,omitempty"`
	Managed  bool   `json:"managed"`
}

func ProcStatus(name string) ProcState {
	procMu.Lock()
	defer procMu.Unlock()
	p := procs[name]
	if p == nil {
		return ProcState{}
	}
	st := ProcState{Running: p.running, PID: p.pid, Restarts: p.restarts, LastErr: p.lastErr, Managed: true}
	if p.running {
		st.Uptime = int64(time.Since(p.started).Seconds())
	}
	return st
}

func ProcRunning(name string) bool { return ProcStatus(name).Running }

func StopAll() {
	procMu.Lock()
	var names []string
	for n := range procs {
		names = append(names, n)
	}
	procMu.Unlock()
	for _, n := range names {
		ProcStop(n)
	}
}

func TailLog(name string, n int) []string { return app.TailFile(LogPath(name), n) }

func rotateLog(p string) {
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(p, p+".1")
	}
}

func appendLog(name, line string) {
	f, err := os.OpenFile(LogPath(name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("15:04:05 ") + line + "\n")
}
