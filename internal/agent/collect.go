package agent

import (
	"os"
	"runtime"

	"localpool/internal/protocol"
)

// Version Agent 版本号
const Version = "0.1.0"

// MachineInfo 节点静态信息（注册用）
type MachineInfo struct {
	Host       string
	OS         string
	Arch       string
	CPUModel   string
	Cores      int
	MemTotalMB uint64
}

// Collector 跨平台资源采集器，内部持有上一次采样用于计算变化率
type Collector struct {
	// 平台相关字段由各平台文件使用
	idle, kernel, user uint64 // CPU 累计(平台单位)
	netRx, netTx       uint64
	netAt              int64
	init               bool
}

// NewCollector 构造采集器
func NewCollector() *Collector {
	return &Collector{}
}

// Sample 采一次样，返回当前机器资源快照
func (c *Collector) Sample() (protocol.MachineStat, error) { return c.sample() }

// ProbeMachine 探测静态机器信息
func ProbeMachine() MachineInfo {
	host, _ := os.Hostname()
	return MachineInfo{
		Host:     host,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUModel: cpuModelName(),
		Cores:    runtime.NumCPU(),
	}
}
