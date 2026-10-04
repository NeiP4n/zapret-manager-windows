package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Job is a long-running background operation with a log the UI tails
// (router: job_start + /tmp/zapret-manager-luci/<job>.log).
type Job struct {
	Name    string
	Label   string
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	lines   []string
	running bool
	failed  bool
	errText string
	started time.Time
	ended   time.Time
}

// Say appends a line to the job log (also mirrored to logs\jobs.log).
func (j *Job) Say(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	j.mu.Lock()
	for _, l := range strings.Split(strings.TrimRight(line, "\n"), "\n") {
		j.lines = append(j.lines, l)
	}
	if len(j.lines) > 4000 {
		j.lines = j.lines[len(j.lines)-3000:]
	}
	j.mu.Unlock()
	Logf("[%s] %s", j.Name, line)
}

func (j *Job) Ctx() context.Context { return j.ctx }

// Stopped reports whether cancellation was requested.
func (j *Job) Stopped() bool { return j.ctx.Err() != nil }

// Write lets a Job be used as an io.Writer for child process output.
func (j *Job) Write(p []byte) (int, error) {
	s := strings.TrimRight(strings.ReplaceAll(string(p), "\r", ""), "\n")
	if s != "" {
		j.Say("%s", s)
	}
	return len(p), nil
}

type JobStatus struct {
	Name     string `json:"job"`
	Label    string `json:"label"`
	Running  bool   `json:"running"`
	Failed   bool   `json:"failed"`
	Error    string `json:"error,omitempty"`
	Exists   bool   `json:"exists"`
	Started  int64  `json:"started"`
	Ended    int64  `json:"ended"`
	LogLines int    `json:"lines"`
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]*Job{}
)

var ErrBusy = fmt.Errorf("операция уже выполняется")

// StartJob runs fn in the background unless a job with the same name is running.
func StartJob(name, label string, fn func(j *Job) error) error {
	jobsMu.Lock()
	if old, ok := jobs[name]; ok && old.isRunning() {
		jobsMu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{Name: name, Label: label, ctx: ctx, cancel: cancel, running: true, started: time.Now()}
	jobs[name] = j
	jobsMu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				j.Say("ОШИБКА: внутренний сбой: %v", r)
				j.finish(fmt.Errorf("%v", r))
			}
		}()
		err := fn(j)
		if err != nil {
			j.Say("ОШИБКА: %v", err)
		}
		j.finish(err)
	}()
	return nil
}

// RunJobSync runs fn as a named job and waits (used by the console menu and scheduled ticks).
func RunJobSync(name, label string, fn func(j *Job) error, echo func(string)) error {
	done := make(chan error, 1)
	err := StartJob(name, label, func(j *Job) error {
		e := fn(j)
		done <- e
		return e
	})
	if err != nil {
		return err
	}
	shown := 0
	for {
		select {
		case e := <-done:
			if echo != nil {
				for _, l := range GetJob(name).linesFrom(shown) {
					echo(l)
				}
			}
			return e
		case <-time.After(300 * time.Millisecond):
			if echo != nil {
				ls := GetJob(name).linesFrom(shown)
				for _, l := range ls {
					echo(l)
				}
				shown += len(ls)
			}
		}
	}
}

func (j *Job) finish(err error) {
	j.mu.Lock()
	j.running = false
	j.ended = time.Now()
	if err != nil {
		j.failed = true
		j.errText = err.Error()
	}
	j.mu.Unlock()
	j.cancel()
}

func (j *Job) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func (j *Job) linesFrom(n int) []string {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if n >= len(j.lines) {
		return nil
	}
	return append([]string(nil), j.lines[n:]...)
}

func GetJob(name string) *Job {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	return jobs[name]
}

func JobState(name string) JobStatus {
	j := GetJob(name)
	if j == nil {
		return JobStatus{Name: name}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	st := JobStatus{Name: name, Label: j.Label, Running: j.running, Failed: j.failed, Error: j.errText,
		Exists: true, Started: j.started.Unix(), LogLines: len(j.lines)}
	if !j.ended.IsZero() {
		st.Ended = j.ended.Unix()
	}
	return st
}

func JobLog(name string, from int) []string {
	return GetJob(name).linesFrom(from)
}

func CancelJob(name string) bool {
	j := GetJob(name)
	if j == nil || !j.isRunning() {
		return false
	}
	j.Say("==> Отмена по запросу")
	j.cancel()
	return true
}

// BusyJob returns the label of any running job (router: _zm_busy_job).
func BusyJob(except ...string) (string, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
outer:
	for n, j := range jobs {
		for _, e := range except {
			if n == e {
				continue outer
			}
		}
		if j.isRunning() {
			return j.Label, true
		}
	}
	return "", false
}

func RunningJobs() []JobStatus {
	jobsMu.Lock()
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	jobsMu.Unlock()
	var out []JobStatus
	for _, n := range names {
		if st := JobState(n); st.Running {
			out = append(out, st)
		}
	}
	return out
}

// ---- logging ----

var logMu sync.Mutex

func Logf(format string, a ...any) {
	line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, a...) + "\n"
	logMu.Lock()
	defer logMu.Unlock()
	p := P("logs", "manager.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// TailFile returns the last n lines of a text file.
func TailFile(p string, n int) []string {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	if len(b) > 256<<10 {
		b = b[len(b)-256<<10:]
	}
	ls := strings.Split(strings.ReplaceAll(strings.TrimRight(string(b), "\n"), "\r", ""), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}
