//go:build linux

package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"localpool/internal/protocol"
)

func cpuModelName() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") {
			if i := strings.Index(line, ":"); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
		if strings.HasPrefix(line, "Processor") {
			if i := strings.Index(line, ":"); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return "unknown"
}

// readCpuJiffies 返回 (idle, total)
func readCpuJiffies() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	line := strings.TrimPrefix(strings.SplitN(string(data), "\n", 2)[0], "cpu ")
	if !strings.HasPrefix(strings.SplitN(string(data), "\n", 2)[0], "cpu ") {
		return 0, 0, nil
	}
	parts := strings.Fields(line)
	var total uint64
	var idle uint64
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		if i == 3 { // idle
			idle = v
		}
		if i == 4 { // iowait，也算空闲
			idle += v
		}
		total += v
	}
	return idle, total, nil
}

func (c *Collector) sample() (protocol.MachineStat, error) {
	st := protocol.MachineStat{}
	idle, total, err := readCpuJiffies()
	if err == nil {
		if c.init && total > c.kernel {
			dTotal := total - c.kernel
			dIdle := idle - c.idle
			st.CPUPercent = float64(dTotal-dIdle) / float64(dTotal) * 100
		} else {
			c.init = true
		}
		c.idle, c.kernel = idle, total
	}

	// 内存
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var memTotal, memAvail uint64
		for _, ln := range strings.Split(string(b), "\n") {
			fs := strings.Fields(ln)
			if len(fs) >= 2 {
				v, _ := strconv.ParseUint(fs[1], 10, 64)
				switch fs[0] {
				case "MemTotal:":
					memTotal = v
				case "MemAvailable:":
					memAvail = v
				}
			}
		}
		st.MemTotalMB = memTotal / 1024
		st.MemUsedMB = (memTotal - memAvail) / 1024
	}

	// 磁盘（根分区）
	var s syscall.Statfs_t
	if err := syscall.Statfs("/", &s); err == nil {
		bs := uint64(s.Bsize)
		st.DiskTotalMB = s.Blocks * bs / (1024 * 1024)
		free := s.Bavail * bs / (1024 * 1024)
		st.DiskUsedMB = st.DiskTotalMB - free
	}

	// 负载
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fs := strings.Fields(string(b))
		if len(fs) > 0 {
			st.Load1, _ = strconv.ParseFloat(fs[0], 64)
		}
	}
	// 运行时间
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		fs := strings.Fields(string(b))
		if len(fs) > 0 {
			if v, err := strconv.ParseFloat(fs[0], 64); err == nil {
				st.UptimeSec = uint64(v)
			}
		}
	}

	// 网络速率（/proc/net/dev 各接口聚合）
	if rx, tx, err := readNetDev(); err == nil {
		now := time.Now().UnixMilli()
		if c.init && c.netAt > 0 && now > c.netAt {
			dt := float64(now-c.netAt) / 1000.0
			if dt > 0 {
				st.NetInBps = uint64(float64(rx-c.netRx) / dt)
				st.NetOutBps = uint64(float64(tx-c.netTx) / dt)
			}
		}
		c.netRx, c.netTx, c.netAt = rx, tx, now
	} else {
		c.netAt = time.Now().UnixMilli()
	}
	return st, nil
}

func readNetDev() (rx, tx uint64, err error) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	for _, ln := range strings.Split(string(b), "\n")[2:] {
		i := strings.Index(ln, ":")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(ln[:i])
		if name == "lo" {
			continue // 忽略回环
		}
		fs := strings.Fields(ln[i+1:])
		if len(fs) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fs[0], 10, 64)
		t, _ := strconv.ParseUint(fs[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx, nil
}
