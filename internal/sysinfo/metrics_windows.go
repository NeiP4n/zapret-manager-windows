//go:build windows

package sysinfo

import (
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	k32              = windows.NewLazySystemDLL("kernel32.dll")
	pGetTickCount64  = k32.NewProc("GetTickCount64")
	pGlobalMemStatus = k32.NewProc("GlobalMemoryStatusEx")
	pGetSystemTimes  = k32.NewProc("GetSystemTimes")
)

func uptime() int64 {
	r, _, _ := pGetTickCount64.Call()
	return int64(r / 1000)
}

type memStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func memory() (uint64, uint64) {
	var m memStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := pGlobalMemStatus.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0, 0
	}
	return m.TotalPhys, m.TotalPhys - m.AvailPhys
}

var (
	cpuMu              sync.Mutex
	lastIdle, lastBusy uint64
	lastAt             time.Time
	lastPct            int
)

func ft(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

// cpuLoad samples GetSystemTimes; the first call measures over 250 ms.
func cpuLoad() int {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	read := func() (idle, busy uint64) {
		var i, k, u windows.Filetime
		pGetSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
		return ft(i), ft(k) + ft(u)
	}
	if lastAt.IsZero() {
		lastIdle, lastBusy = read()
		time.Sleep(250 * time.Millisecond)
	}
	idle, total := read()
	di, dt := idle-lastIdle, total-lastBusy
	lastIdle, lastBusy, lastAt = idle, total, time.Now()
	if dt > 0 {
		lastPct = int(100 - di*100/dt)
	}
	return lastPct
}
