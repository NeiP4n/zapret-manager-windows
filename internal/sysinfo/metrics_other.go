//go:build !windows

package sysinfo

func uptime() int64            { return 3600 * 5 }
func memory() (uint64, uint64) { return 16 << 30, 7 << 30 }
func cpuLoad() int             { return 12 }
