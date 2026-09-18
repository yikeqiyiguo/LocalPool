//go:build linux

package agent

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var lastXIdleAt time.Time
var lastXIdleVal float64

// humanIdleSec 返回距离最近一次人机交互的秒数。
// Linux 下优先使用 xprintidle（桌面环境），无法检测时返回 -1 表示“视为空闲”。
func humanIdleSec() float64 {
	path, err := exec.LookPath("xprintidle")
	if err != nil {
		return -1
	}
	// 避免频繁 fork：缓存 5s
	if time.Since(lastXIdleAt) < 5*time.Second {
		return lastXIdleVal
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		return -1
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return -1
	}
	lastXIdleAt = time.Now()
	lastXIdleVal = v / 1000.0
	return lastXIdleVal
}
