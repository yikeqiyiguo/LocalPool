//go:build windows

package agent

import (
	"os"
	"syscall"
	"unsafe"

	"localpool/internal/protocol"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes           = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx     = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW      = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetTickCount64           = kernel32.NewProc("GetTickCount64")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procGetProcessTimes          = kernel32.NewProc("GetProcessTimes")
	procSetPriorityClass         = kernel32.NewProc("SetPriorityClass")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32.NewProc("Process32NextW")
	psapi                        = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMemoryInfo     = psapi.NewProc("GetProcessMemoryInfo")
	procK32GetProcessMemoryInfo  = psapi.NewProc("K32GetProcessMemoryInfo")
	user32                       = syscall.NewLazyDLL("user32.dll")
	procGetLastInputInfo         = user32.NewProc("GetLastInputInfo")
)

// processEntry32W 与 PROCESSENTRY32W 对齐
type processEntry32W struct {
	size              uint32
	cntUsage          uint32
	th32ProcessID     uint32
	th32DefaultHeapID uintptr
	th32ModuleID      uint32
	cntThreads        uint32
	th32ParentPID     uint32
	priClassBase      int32
	flags             uint32
	exeFile           [260]uint16
}

type filetime struct {
	low  uint32
	high uint32
}

type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uint64
	workingSetSize             uint64
	quotaPeakPagedPoolUsage    uint64
	quotaPagedPoolUsage        uint64
	quotaPeakNonPagedPoolUsage uint64
	quotaNonPagedPoolUsage     uint64
	pagefileUsage              uint64
	peakPagefileUsage          uint64
}

func cpuModelName() string {
	if m := os.Getenv("PROCESSOR_IDENTIFIER"); m != "" {
		return m
	}
	return "x86-compatible"
}

func getCpuTimes() (idle, kernel, user uint64) {
	var i, k, u filetime
	procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&i)),
		uintptr(unsafe.Pointer(&k)),
		uintptr(unsafe.Pointer(&u)),
	)
	idle = uint64(i.low) | uint64(i.high)<<32
	kernel = uint64(k.low) | uint64(k.high)<<32
	user = uint64(u.low) | uint64(u.high)<<32
	return
}

func (c *Collector) sample() (protocol.MachineStat, error) {
	st := protocol.MachineStat{}

	idle, kernel, user := getCpuTimes()
	total := kernel + user // kernel 已包含 idle
	if c.init {
		dTotal := total - c.kernel
		dIdle := idle - c.idle
		if dTotal > 0 {
			st.CPUPercent = float64(dTotal-dIdle) / float64(dTotal) * 100
		}
	} else {
		c.init = true
	}
	c.idle, c.kernel, c.user = idle, total, user

	// 内存
	var mse memoryStatusEx
	mse.length = uint32(unsafe.Sizeof(mse))
	procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mse)))
	st.MemTotalMB = mse.totalPhys / (1024 * 1024)
	st.MemUsedMB = (mse.totalPhys - mse.availPhys) / (1024 * 1024)

	// 磁盘（系统盘）
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	root, _ := syscall.UTF16PtrFromString(drive + `\`)
	var freeBytesAvail, totalBytes, totalFree uint64
	r1, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&freeBytesAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 != 0 {
		st.DiskTotalMB = totalBytes / (1024 * 1024)
		st.DiskUsedMB = (totalBytes - freeBytesAvail) / (1024 * 1024)
	}

	// 运行时间
	var ms uint64
	procGetTickCount64.Call(uintptr(unsafe.Pointer(&ms)))
	st.UptimeSec = ms / 1000
	st.Load1 = 0
	st.NetInBps = 0
	st.NetOutBps = 0
	return st, nil
}
