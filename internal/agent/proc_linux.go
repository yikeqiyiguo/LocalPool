//go:build linux

package agent

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"localpool/internal/protocol"
)

// buildTaskCmd 构建 bash -lc 命令，进程独立进程组便于整树终止
func buildTaskCmd(t protocol.AssignTask) (*exec.Cmd, error) {
	shell := t.Shell
	name := "bash"
	args := []string{"-lc", t.Command}
	if shell == "sh" || shell == "auto" && !hasBinary("bash") {
		name = "/bin/sh"
		args = []string{"-c", t.Command}
	}
	if shell == "python" {
		name = "python3"
		args = []string{"-c", t.Command}
	}
	cmd := exec.Command(name, args...)
	if t.Cwd != "" {
		cmd.Dir = t.Cwd
	}
	cmd.Env = mergedEnv(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// killTaskProc 终止整个进程组
func killTaskProc(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("进程不存在")
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}

// throttleProc Linux 下 soft limit 通过 nice 降低优先级（尽力而为）
func throttleProc(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	return nil
}

// sampleProcTree 递归采样整棵进程树 CPU(ns)/RSS(KB)
func sampleProcTree(cmd *exec.Cmd) (cpuNs uint64, rssKB uint64, err error) {
	if cmd == nil || cmd.Process == nil {
		return 0, 0, errors.New("进程不存在")
	}
	kids, err := collectChildPids(cmd.Process.Pid)
	if err != nil {
		return 0, 0, err
	}
	for _, pid := range kids {
		c, _ := readProcSchedNs(pid)
		r, _ := readProcRSS(pid)
		cpuNs += c
		rssKB += r
	}
	return cpuNs, rssKB, nil
}

// collectChildPids 广度优先收集 pid 的所有后代
func collectChildPids(root int) ([]int, error) {
	procDir, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parent := map[int]int{}
	all := []int{}
	for _, d := range procDir {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		ppid, err := readProcPPid(pid)
		if err == nil {
			parent[pid] = ppid
		}
		all = append(all, pid)
	}
	result := []int{root}
	queue := []int{root}
	visited := map[int]bool{root: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, pid := range all {
			if !visited[pid] && parent[pid] == cur {
				visited[pid] = true
				result = append(result, pid)
				queue = append(queue, pid)
			}
		}
	}
	return result, nil
}

func readProcPPid(pid int) (int, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 > len(s) {
		return 0, errors.New("bad stat")
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, errors.New("bad stat fields")
	}
	return strconv.Atoi(fields[1]) // state, ppid
}

// readProcSchedNs /proc/<pid>/schedstat 第一字段为 CPU 累计时间(纳秒)
func readProcSchedNs(pid int) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/schedstat")
	if err != nil {
		return 0, err
	}
	fs := strings.Fields(string(data))
	if len(fs) == 0 {
		return 0, errors.New("empty schedstat")
	}
	return strconv.ParseUint(fs[0], 10, 64)
}

// readProcRSS /proc/<pid>/status 的 VmRSS(kB)
func readProcRSS(pid int) (uint64, error) {
	f, err := os.Open("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fs := strings.Fields(line)
			if len(fs) >= 2 {
				return strconv.ParseUint(fs[1], 10, 64)
			}
		}
	}
	return 0, errors.New("VmRSS not found")
}
