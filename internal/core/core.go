// Package core boots the manager inside the service: restores every enabled component, runs the
// scheduler (the router's cron jobs) and puts the system back in order on shutdown.
package core

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/byetube"
	"github.com/zapretmanager/zmwin/internal/doh"
	"github.com/zapretmanager/zmwin/internal/osx"
	"github.com/zapretmanager/zmwin/internal/routing"
	"github.com/zapretmanager/zmwin/internal/tg"
	"github.com/zapretmanager/zmwin/internal/web"
	"github.com/zapretmanager/zmwin/internal/zapret"
)

// Run starts the panel and all components; it blocks until ctx is cancelled.
func Run(ctx context.Context) error {
	app.EnsureDirs()
	secureState()
	_ = web.Token()
	app.Logf("Zapret Manager %s starting (base %s)", app.Version, app.Base)

	errc := make(chan error, 1)
	go func() { errc <- web.Serve(ctx) }()

	go boot()
	go scheduler(ctx)

	select {
	case <-ctx.Done():
	case err := <-errc:
		if err != nil {
			Shutdown()
			return err
		}
	}
	Shutdown()
	return nil
}

func boot() {
	defer func() {
		if r := recover(); r != nil {
			app.Logf("boot panic: %v", r)
		}
	}()
	zapret.RecoverTest()
	if err := zapret.Restart(); err != nil {
		app.Logf("zapret: %v", err)
	}
	if err := zapret.Restart2(); err != nil {
		app.Logf("zapret2: %v", err)
	}
	if err := doh.Apply(); err != nil {
		app.Logf("doh: %v", err)
	}
	tg.Boot()
	byetube.Boot()
	routing.Boot()
}

// Shutdown stops helpers and restores DNS / proxy settings so the PC keeps working without us.
func Shutdown() {
	app.Logf("shutdown")
	byetube.Shutdown()
	doh.Shutdown()
	routing.Stop()
	osx.StopAll()
}

// scheduler replaces the router's crontab entries.
func scheduler(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	lastMinute := ""
	lastDNS := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			minute := now.Format("15:04")
			if minute == lastMinute {
				continue
			}
			lastMinute = minute
			safe("tg", tg.Tick)
			safe("routing", func() { routing.Tick(ctx, now) })
			if time.Since(lastDNS) > time.Minute {
				lastDNS = time.Now()
				safe("doh", doh.Watch)
			}
			s := app.S()
			// exclusion list refresh: hN = every N hours at :17
			if s.ExclAuto != "" && s.ExclAuto != "off" && now.Minute() == 17 {
				var h int
				fmt.Sscanf(s.ExclAuto, "h%d", &h)
				if h > 0 && now.Hour()%h == 0 {
					go safe("excl", func() { zapret.ExclTick(ctx) })
				}
			}
			// nightly auto-best (test_auto_tick)
			if s.AutobestTime != "" && minute == s.AutobestTime && zapret.Installed() && s.ZapretEnabled {
				if _, busy := app.BusyJob(); !busy {
					_ = app.StartJob("strategy_test", "Автоподбор стратегии", zapret.AutoBest)
				}
			}
		}
	}
}

func safe(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			app.Logf("scheduler %s panic: %v", name, r)
		}
	}()
	fn()
}

// secureState limits state\ (token, keys, configs) to Administrators and SYSTEM.
func secureState() {
	if app.Dev {
		return
	}
	marker := app.P("state", ".acl")
	if app.Exists(marker) {
		return
	}
	_, err := osx.Run(nil, 30*time.Second, "icacls", app.StateDir, "/inheritance:r", "/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
	if err == nil {
		_ = os.MkdirAll(app.P("awg"), 0o700)
		_, _ = osx.Run(nil, 30*time.Second, "icacls", app.P("awg"), "/inheritance:r", "/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
		app.Touch(marker)
	}
}
