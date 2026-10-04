//go:build !windows

package main

func isService() bool { return false }

// runService on non-Windows (dev mode) just runs in the foreground.
func runService() { _ = runForeground() }
