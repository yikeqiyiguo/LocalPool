// Package agent LocalPool 节点 Agent：资源采集、空闲检测、任务执行、日志回传。
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"localpool/internal/protocol"
)

// Agent 节点客户端主体
type Agent struct {
	cfg *Config
	mi  MachineInfo
	col *Collector
	cli *client

	nodeID    atomic.Value // string
	beatMs    atomic.Int64
	idleThr   atomic.Int64 // 人机空闲阈值秒
	humanIdle atomic.Value // float64

	mu      sync.Mutex
	running map[string]*procState

	quit chan struct{}
}

// procState 单任务运行状态
type procState struct {
	task     protocol.AssignTask
	cmd      any // *exec.Cmd，平台各异避免 import 环
	startMs  int64
	stop     chan struct{}
	stopOnce sync.Once

	mu         sync.Mutex
	lastCpuPct float64
	lastMemMB  float64
	lastCpuNs  uint64
	peakMemKB  uint64
}

// NewAgent 创建 Agent
func NewAgent(cfg *Config, mi MachineInfo) *Agent {
	a := &Agent{
		cfg:     cfg,
		mi:      mi,
		col:     NewCollector(),
		running: map[string]*procState{},
		quit:    make(chan struct{}),
	}
	a.nodeID.Store(cfg.NodeID)
	a.beatMs.Store(int64(2000))
	a.idleThr.Store(int64(300))
	a.humanIdle.Store(float64(-1))
	return a
}

// AttachClient 注入 HTTP 客户端（main 中调用）
func (a *Agent) AttachClient(server, secret string) {
	a.cli = newClient(server, secret)
}

// Run 主流程：注册 -> 心跳循环（内含任务领取）
func (a *Agent) Run(ctx context.Context) error {
	if err := a.register(ctx); err != nil {
		return fmt.Errorf("注册到调度中心失败: %w", err)
	}
	log.Printf("[LocalPool] 已注册节点 %s (%s), 开始心跳上报", a.cfg.NodeID, a.mi.Host)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err := a.heartbeatOnce(ctx)
		if err != nil {
			log.Printf("[LocalPool] 心跳异常: %v", err)
			// 若是要求重新注册则立即重注册
			if errors.Is(err, errReRegister) {
				if rerr := a.register(ctx); rerr != nil {
					log.Printf("[LocalPool] 重新注册失败: %v", rerr)
				}
			}
		}
		ms := a.beatMs.Load()
		if ms < 500 {
			ms = 2000
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(ms) * time.Millisecond):
		}
	}
}

var errReRegister = errors.New("require re-register")

// register 注册/重注册节点
func (a *Agent) register(ctx context.Context) error {
	req := protocol.RegisterReq{
		ID:           a.cfg.NodeID,
		Name:         a.cfg.Name,
		Host:         a.mi.Host,
		OS:           a.mi.OS,
		Arch:         a.mi.Arch,
		CPUModel:     a.mi.CPUModel,
		Cores:        a.mi.Cores,
		MemTotalMB:   a.mi.MemTotalMB,
		AgentVersion: Version,
	}
	var resp protocol.RegisterResp
	if _, err := a.cli.postJSON(ctx, "/agent/register", req, &resp); err != nil {
		return err
	}
	a.cfg.NodeID = resp.NodeID
	a.nodeID.Store(resp.NodeID)
	a.beatMs.Store(int64(resp.HeartbeatMs))
	a.idleThr.Store(int64(resp.HumanIdleThresholdSec))
	return a.cfg.Save()
}

// heartbeatOnce 一次心跳：采集+上报+处理任务指令
func (a *Agent) heartbeatOnce(ctx context.Context) error {
	nodeID, _ := a.nodeID.Load().(string)
	if nodeID == "" {
		return errReRegister
	}
	st, err := a.col.Sample()
	if err != nil {
		log.Printf("[LocalPool] 采样失败: %v", err)
	}

	idleSec := humanIdleSec()
	a.humanIdle.Store(idleSec)
	threshold := a.idleThr.Load()
	humanActive := false
	reason := ""
	if idleSec >= 0 && float64(threshold) > 0 && idleSec < float64(threshold) {
		humanActive = true
		reason = "检测到键盘/鼠标活动"
	}

	req := protocol.HeartbeatReq{
		NodeID:      nodeID,
		Version:     Version,
		HumanActive: humanActive,
		HumanReason: reason,
		IdleSec:     idleSec,
		Stat:        st,
		Running:     a.snapshotRunning(),
	}
	var resp protocol.HeartbeatResp
	code, err := a.cli.postJSON(ctx, "/agent/heartbeat", req, &resp)
	if err != nil {
		if code == 409 {
			return errReRegister
		}
		return err
	}
	if resp.IntervalMs > 0 {
		a.beatMs.Store(int64(resp.IntervalMs))
	}
	if resp.HumanIdleThresholdSec > 0 {
		a.idleThr.Store(int64(resp.HumanIdleThresholdSec))
	}
	for _, t := range resp.Assign {
		a.startTask(ctx, t)
	}
	for _, id := range resp.Cancel {
		a.cancelTask(id)
	}
	return nil
}

// startTask 领取一个下发任务
func (a *Agent) startTask(ctx context.Context, t protocol.AssignTask) {
	a.mu.Lock()
	if _, ok := a.running[t.ID]; ok {
		a.mu.Unlock()
		return
	}
	ps := &procState{task: t, startMs: protocol.NowMs(), stop: make(chan struct{})}
	a.running[t.ID] = ps
	a.mu.Unlock()
	log.Printf("[LocalPool] 开始执行任务 %s: %s", t.ID, truncateStr(t.Command, 80))
	go a.runTask(ctx, ps)
}

// cancelTask 请求终止任务
func (a *Agent) cancelTask(taskID string) {
	a.mu.Lock()
	ps := a.running[taskID]
	a.mu.Unlock()
	if ps == nil {
		return
	}
	log.Printf("[LocalPool] 收到取消指令 %s", taskID)
	ps.stopOnce.Do(func() { close(ps.stop) })
}

// snapshotRunning 汇总运行中任务状态用于心跳
func (a *Agent) snapshotRunning() []protocol.RunningTaskStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]protocol.RunningTaskStatus, 0, len(a.running))
	for _, ps := range a.running {
		ps.mu.Lock()
		out = append(out, protocol.RunningTaskStatus{
			TaskID:      ps.task.ID,
			PID:         0,
			CPUPercent:  ps.lastCpuPct,
			MemMB:       uint64(ps.lastMemMB),
			StartedAtMs: ps.startMs,
			ElapsedSec:  float64(protocol.NowMs()-ps.startMs) / 1000,
		})
		ps.mu.Unlock()
	}
	return out
}

// currentNodeID 取当前节点 ID。节点可能经历注册/重注册，NodeID 统一从原子值读取，
// 避免后台日志回传等协程与主协程并发读 a.cfg 造成数据竞态。
func (a *Agent) currentNodeID() string {
	if v, ok := a.nodeID.Load().(string); ok {
		return v
	}
	return ""
}

// truncateStr 截断长命令用于日志
func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// mergedEnv 组装进程环境变量（系统环境 + 任务自定义 + 任务标识）
func mergedEnv(t protocol.AssignTask) []string {
	base := os.Environ()
	set := map[string]string{}
	for _, kv := range base {
		i := strings.Index(kv, "=")
		if i > 0 {
			set[kv[:i]] = kv[i+1:]
		}
	}
	set["LOCALPOOL_TASK_ID"] = t.ID
	set["LP_TASK_ID"] = t.ID
	for k, v := range t.Env {
		set[k] = v
	}
	out := make([]string, 0, len(set))
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}
