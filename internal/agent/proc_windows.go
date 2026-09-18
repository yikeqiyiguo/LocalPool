//go:build windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"localpool/internal/protocol"
)

const (
	procQueryLimitedInfo = 0x1000
	procQueryInfo        = 0x0400
	procSetInformation   = 0x0200
	idlePriorityClass    = 0x00000040
	createNoWindow       = 0x08000000
)

// buildTaskCmd 按平台与任务声明的 shell 构建命令
func buildTaskCmd(t protocol.AssignTask) (*exec.Cmd, error) {
	var name string
	var args []string
	switch t.Shell {
	case "powershell", "pwsh":
		name = "powershell.exe"
		args = []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", t.Command}
	default:
		name = os.Getenv("COMSPEC")
		if name == "" {
			name = "cmd.exe"
		}
		args = []string{"/C", t.Command}
	}
	cmd := exec.Command(name, args...)
	if t.Cwd != "" {
		cmd.Dir = t.Cwd
	}
	cmd.Env = mergedEnv(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return cmd, nil
}

// killTaskProc 终止进程树（含子进程）
func killTaskProc(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("进程不存在")
	}
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	_ = killer.Run()
	_ = cmd.Process.Kill()
	return nil
}

// throttleProc 降低进程优先级（CPU 软限制手段）
func throttleProc(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	h, _, _ := procOpenProcess.Call(procSetInformation, 0, uintptr(cmd.Process.Pid))
	if h == 0 {
		return errors.New("OpenProcess 失败")
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	procSetPriorityClass.Call(h, idlePriorityClass)
	return nil
}

const th32csSnapProcess = 0x2 // TH32CS_SNAPPROCESS

// sampleProcTree 递归采样整棵进程树 CPU(累计纳秒)/RSS(KB)。
// 任务外壳(cmd/powershell)只负责等待，真实负载在其子进程中，
// 因此不能只读根进程，需借助 Toolhelp32 快照枚举全部后代。
func sampleProcTree(cmd *exec.Cmd) (cpuNs uint64, rssKB uint64, err error) {
	if cmd == nil || cmd.Process == nil {
		return 0, 0, errors.New("进程不存在")
	}
	root := uint32(cmd.Process.Pid)
	parent := collectWinParents()
	if parent == nil {
		// 快照失败时退化为仅根进程采样
		c, r := sampleWinProc(root)
		return c * 100, r, nil
	}
	for _, pid := range collectWinDescendants(root, parent) {
		c, r := sampleWinProc(pid)
		cpuNs += c * 100 // 100ns -> ns
		rssKB += r
	}
	return cpuNs, rssKB, nil
}

// collectWinParents 通过 CreateToolhelp32Snapshot 枚举进程，建立 pid -> ppid 映射
func collectWinParents() map[uint32]uint32 {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == 0 || snap == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return nil
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	parent := map[uint32]uint32{}
	var e processEntry32W
	e.size = uint32(unsafe.Sizeof(e))
	if r1, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e))); r1 == 0 {
		return nil
	}
	for {
		parent[e.th32ProcessID] = e.th32ParentPID
		if r1, _, _ := procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e))); r1 == 0 {
			break
		}
	}
	return parent
}

// collectWinDescendants 广度优先收集 root 的全部后代（含 root 自身）
func collectWinDescendants(root uint32, parent map[uint32]uint32) []uint32 {
	result := []uint32{root}
	visited := map[uint32]bool{root: true}
	queue := []uint32{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for pid, ppid := range parent {
			if !visited[pid] && ppid == cur {
				visited[pid] = true
				result = append(result, pid)
				queue = append(queue, pid)
			}
		}
	}
	return result
}

// sampleWinProc 采样单个进程：CPU(100ns tick) 与 RSS(KB)；无权访问时返回 0
func sampleWinProc(pid uint32) (cpu100ns, rssKB uint64) {
	h, _, _ := procOpenProcess.Call(procQueryInfo|procQueryLimitedInfo, 0, uintptr(pid))
	if h == 0 {
		return 0, 0
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))
	procK32GetProcessMemoryInfo.Call(h, uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
	if counters.workingSetSize > 0 {
		rssKB = counters.workingSetSize / 1024
	}

	var ct, et, kt, ut filetime
	if r1, _, _ := procGetProcessTimes.Call(h,
		uintptr(unsafe.Pointer(&ct)),
		uintptr(unsafe.Pointer(&et)),
		uintptr(unsafe.Pointer(&kt)),
		uintptr(unsafe.Pointer(&ut))); r1 != 0 {
		cpu100ns = (uint64(kt.low) | uint64(kt.high)<<32) + (uint64(ut.low) | uint64(ut.high)<<32)
	}
	return
}
