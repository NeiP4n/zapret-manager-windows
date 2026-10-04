//go:build windows

package main

import (
	"context"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/zapretmanager/zmwin/internal/app"
	"github.com/zapretmanager/zmwin/internal/core"
)

func isService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

type handler struct{}

func (handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- core.Run(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending, WaitHint: 20000}
				cancel()
				select {
				case <-done:
				case <-time.After(18 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				app.Logf("service: %v", err)
				return false, 1
			}
			return false, 0
		}
	}
}

func runService() {
	if !isService() {
		_ = runForeground()
		return
	}
	if err := svc.Run(app.ServiceName, handler{}); err != nil {
		app.Logf("svc.Run: %v", err)
	}
}
