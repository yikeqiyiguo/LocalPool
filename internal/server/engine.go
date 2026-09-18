package server

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"localpool/internal/protocol"
)

// Engine 调度核心：节点心跳判定、任务贪心调度、人机共存策略
type Engine struct {
	store    *Store
	hub      *LogHub
	settings atomic.Value // Settings

	mu      sync.Mutex
	pending map[string][]protocol.AssignTask // nodeID -> 待下发的任务
	cancels map[string][]string              // nodeID -> 待取消任务
}

// NewEngine 初始化调度引擎
func NewEngine(store *Store, hub *LogHub) *Engine {
	e := &Engine{
		store:   store,
		hub:     hub,
		pending: map[string][]protocol.AssignTask{},
		cancels: map[string][]string{},
	}
	e.settings.Store(store.LoadSettings())
	return e
}

// Settings 当前设置副本
func (e *Engine) Settings() Settings {
	return e.settings.Load().(Settings)
}

// UpdateSettings 更新并持久化设置
func (e *Engine) UpdateSettings(st Settings) error {
	st = ParseSettings(st.Serialize())
	e.settings.Store(st)
	return e.store.SaveSettings(st)
}

// Run 启动调度循环（间隔取自当前设置，运行期修改 DispatchIntervalMs 即时生效）
func (e *Engine) Run(ctx context.Context) {
	for {
		interval := time.Duration(e.Settings().DispatchIntervalMs) * time.Millisecond
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			e.tick()
		}
	}
}

// tick 一轮调度
func (e *Engine) tick() {
	now := time.Now().UnixMilli()
	st := e.Settings()

	// 1. 离线检测
	e.detectOffline(now, st)

	// 2. 人机共存：节点被占用时终止 need_idle 任务
	e.evictHumanActive(now, st)

	// 3. 贪心分发排队任务
	e.dispatch(now, st)
}

// detectOffline 标记离线节点，并失败其上的运行任务
func (e *Engine) detectOffline(now int64, st Settings) {
	nodes, err := e.store.ListNodes(now, int64(st.OfflineGraceMs))
	if err != nil {
		return
	}
	for _, n := range nodes {
		if !n.Online {
			continue
		}
		// DB 层 last_seen 是否真超时
		if n.LastSeen > 0 && now-n.LastSeen > int64(st.OfflineGraceMs) {
			_ = e.store.MarkNodeOffline(n.ID)
			// 丢弃积压的下发/取消指令，防止节点回归后执行已在 DB 失效的任务
			e.DropPending(n.ID)
			tasks, _ := e.store.QueryRunningOnNode(n.ID)
			for _, t := range tasks {
				if now-t.StartedAt < int64(st.OfflineGraceMs) {
					continue // 刚分发，可能正在启动
				}
				msg := "执行节点已离线(心跳超时)"
				if ok, _ := e.store.FailTask(t.ID, protocol.ReasonNodeLost, msg); ok {
					e.autoRetry(t, st)
				}
			}
		}
	}
}

// evictHumanActive 若策略开启，终止运行中被占用机器上的 need_idle 任务
func (e *Engine) evictHumanActive(now int64, st Settings) {
	if !st.HumanEnabled || !st.EvictOnHumanActive {
		return
	}
	nodes, err := e.store.ListNodes(now, int64(st.OfflineGraceMs))
	if err != nil {
		return
	}
	for _, n := range nodes {
		if !n.Online || !n.HumanActive {
			continue
		}
		tasks, err := e.store.QueryRunningOnNode(n.ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.NeedIdle {
				msg := fmt.Sprintf("检测到 %s 正在被使用，任务已按人机共存策略终止", n.Name)
				if ok, _ := e.store.FailTask(t.ID, protocol.ReasonHumanEvict, msg); ok {
					e.EnqueueCancel(n.ID, t.ID)
					e.autoRetry(t, st)
				}
			}
		}
	}
}

// autoRetry 满足条件时自动重试（用于节点丢失/占用等异常）
func (e *Engine) autoRetry(orig *Task, st Settings) {
	if st.MaxRetries <= 0 || orig.RetryCount >= st.MaxRetries {
		return
	}
	nt, err := e.store.RetryTask(orig)
	if err == nil {
		e.publishTaskEvent(nt, "created")
	}
}

// dispatch 贪心调度：从排队任务中挑选节点下发
func (e *Engine) dispatch(now int64, st Settings) {
	queued, err := e.store.QueryQueuedTasks(100)
	if err != nil || len(queued) == 0 {
		return
	}
	nodes, err := e.store.ListNodes(now, int64(st.OfflineGraceMs))
	if err != nil {
		return
	}
	runCounts, err := e.store.CountRunningByNode()
	if err != nil {
		return
	}
	online := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Online {
			online = append(online, n)
		}
	}
	if len(online) == 0 {
		return
	}
	// 按运行任务数升序 -> CPU 空闲率降序，形成"最闲优先"
	sort.SliceStable(online, func(i, j int) bool {
		ci, cj := runCounts[online[i].ID], runCounts[online[j].ID]
		if ci != cj {
			return ci < cj
		}
		return online[i].CPUFreePercent > online[j].CPUFreePercent
	})

	for _, t := range queued {
		// HumanEnabled 是人机共存总开关：关闭后不避让、不要求"空闲机"，
		// need_idle 任务按普通任务任意调度，避免被永久卡在队列。
		var target *Node
		for _, n := range online {
			if n.HumanActive && t.NeedIdle {
				continue
			}
			if runCounts[n.ID] >= st.MaxTasksPerNode {
				continue
			}
			// CPU 软约束：任务上限按"单核=100%"(与 Agent 采样口径一致)，
			// 节点侧按空闲核容量估算：空闲核% = (100-整机占用%)*核数。
			if t.MaxCPUPercent > 0 {
				freeCorePct := n.CPUFreePercent * float64(n.Cores)
				if freeCorePct < float64(t.MaxCPUPercent)*0.8 {
					continue
				}
			}
			// 内存软约束
			if t.MaxMemMB > 0 && uint64(t.MaxMemMB) > n.MemFreeMB {
				continue
			}
			target = n
			break
		}
		if target == nil {
			// 当前任务无可匹配节点（资源不够/需空闲机），跳过它继续尝试后续任务，
			// 避免单个大头/need_idle 任务阻塞整条队列（队头阻塞）。
			continue
		}
		// 先以 CAS 抢占(queued->running)：若任务在排队期间已被取消/删除，
		// StartTask 会返回 false，跳过下发，避免"已取消任务被实际执行"。
		started, err := e.store.StartTask(t.ID, target.ID, target.Name, now)
		if err != nil || !started {
			continue
		}
		e.EnqueueAssign(target.ID, t.toAssign())
		runCounts[target.ID]++
		e.publishTaskEvent(t, "started")
		// 重新排序节点（当前节点刚被占一个槽位）
		sort.SliceStable(online, func(i, j int) bool {
			ci, cj := runCounts[online[i].ID], runCounts[online[j].ID]
			if ci != cj {
				return ci < cj
			}
			return online[i].CPUFreePercent > online[j].CPUFreePercent
		})
	}
}

// EnqueueAssign 排队下发任务（心跳带回）
func (e *Engine) EnqueueAssign(nodeID string, at protocol.AssignTask) {
	e.mu.Lock()
	e.pending[nodeID] = append(e.pending[nodeID], at)
	e.mu.Unlock()
}

// EnqueueCancel 排队取消
func (e *Engine) EnqueueCancel(nodeID, taskID string) {
	e.mu.Lock()
	e.cancels[nodeID] = protocol.AppendIfMissing(e.cancels[nodeID], taskID)
	e.mu.Unlock()
}

// DrainFor 心跳时取走该节点待下发/待取消指令
func (e *Engine) DrainFor(nodeID string) (assign []protocol.AssignTask, cancels []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	assign = e.pending[nodeID]
	delete(e.pending, nodeID)
	cancels = e.cancels[nodeID]
	delete(e.cancels, nodeID)
	return
}

// CancelTask 用户取消任务
func (e *Engine) CancelTask(taskID string, reason string) error {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	switch t.Status {
	case protocol.StatusQueued:
		ok, err := e.store.CancelQueued(taskID, protocol.ReasonUserCancel)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		// CAS 未命中：任务极可能刚被调度领取为 running，升级为运行中取消。
		// （若已被他人取消/已终态，则无需再处理。）
		if cur, err := e.store.GetTask(taskID); err == nil && cur.Status == protocol.StatusRunning {
			if err := e.store.CancelRunningTask(taskID, protocol.ReasonUserCancel, "用户已请求终止"); err != nil {
				return err
			}
			if cur.NodeID != "" {
				e.EnqueueCancel(cur.NodeID, taskID)
			}
		}
		return nil
	case protocol.StatusRunning:
		// 先落 DB（带 status 保护），确保取消状态持久化后再下发指令。
		// 即使此时崩溃，Agent 恢复心跳后仍会通过对账补发取消，避免进程永远存活。
		if err := e.store.CancelRunningTask(taskID, reason, "用户已请求终止"); err != nil {
			return err
		}
		if t.NodeID != "" {
			e.EnqueueCancel(t.NodeID, taskID)
		}
		return nil
	default:
		return fmt.Errorf("任务当前状态为 %s，不可取消", t.Status)
	}
}

// RetryTask 手动重试
func (e *Engine) RetryTask(taskID string) (*Task, error) {
	orig, err := e.store.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("任务不存在")
	}
	nt, err := e.store.RetryTask(orig)
	if err != nil {
		return nil, err
	}
	e.publishTaskEvent(nt, "created")
	return nt, nil
}

// ReconcileHeartbeat 心跳对账：
//  1. Agent 已跑但 DB 仍 queued（服务重启丢下发记录）-> 认领为 running；
//  2. Agent 上报了 DB 已处于终态/被迁移的任务（取消指令丢失、节点离线误判后恢复等）
//     -> 补发取消，消除"取消/失败后进程仍存活"的僵尸执行与重复执行；
//  3. DB running 但 Agent 不承认且超过启动宽限 -> 视为进程丢失。
func (e *Engine) ReconcileHeartbeat(nodeID, nodeName string, reported []protocol.RunningTaskStatus, justAssigned map[string]bool) {
	now := time.Now().UnixMilli()
	for _, r := range reported {
		t, err := e.store.GetTask(r.TaskID)
		if err != nil {
			continue
		}
		switch t.Status {
		case protocol.StatusQueued:
			_ = e.store.ClaimRunning(r.TaskID, nodeID, nodeName, now)
			e.publishTaskEvent(t, "started")
		case protocol.StatusRunning:
			if t.NodeID != nodeID {
				// 同一任务不应同时在两个节点运行，异常副本立即终止
				e.EnqueueCancel(nodeID, r.TaskID)
			}
		case protocol.StatusFailed, protocol.StatusCanceled:
			if t.NodeID == nodeID {
				// DB 已终态但本节点进程仍存活：取消指令此前可能因重启/离线丢失，补发
				e.EnqueueCancel(nodeID, r.TaskID)
			}
		}
	}

	st := e.Settings()
	missing, err := e.store.QueryRunningOnNodeNotIn(nodeID, runningIDSet(reported), int64(st.OfflineGraceMs))
	if err != nil {
		return
	}
	for _, t := range missing {
		if justAssigned[t.ID] {
			continue // 刚在本轮下发，Agent 下一次心跳才会上报
		}
		if e.assignPending(nodeID, t.ID) {
			continue // 已下发仍在等待领取，不能视为进程丢失
		}
		msg := "节点上未发现任务进程，可能已被异常回收"
		if ok, _ := e.store.FailTask(t.ID, protocol.ReasonNodeLost, msg); ok {
			e.autoRetry(t, st)
		}
	}
}

// runningIDSet 提取上报任务的 id 集合
func runningIDSet(reported []protocol.RunningTaskStatus) map[string]bool {
	set := make(map[string]bool, len(reported))
	for _, r := range reported {
		set[r.TaskID] = true
	}
	return set
}

// assignPending 判断某任务是否仍在待下发队列中（等待 Agent 心跳领取）
func (e *Engine) assignPending(nodeID, taskID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, at := range e.pending[nodeID] {
		if at.ID == taskID {
			return true
		}
	}
	return false
}

// PruneAssign 过滤下发任务：仅保留 DB 中仍在本节点 running 的任务，
// 丢弃已被取消/失败/删除的陈旧下发，避免 Agent 执行无效任务
func (e *Engine) PruneAssign(nodeID string, assign []protocol.AssignTask) []protocol.AssignTask {
	if len(assign) == 0 {
		return nil
	}
	out := make([]protocol.AssignTask, 0, len(assign))
	for _, at := range assign {
		t, err := e.store.GetTask(at.ID)
		if err != nil || t.NodeID != nodeID || t.Status != protocol.StatusRunning {
			continue
		}
		out = append(out, at)
	}
	return out
}

// DropPending 节点离线时丢弃积压的下发/取消指令（该节点短期无法领取）
func (e *Engine) DropPending(nodeID string) {
	e.mu.Lock()
	delete(e.pending, nodeID)
	delete(e.cancels, nodeID)
	e.mu.Unlock()
}

// publishTaskEvent 记录变更通知（目前仅作钩子，便于后续扩展 SSE 事件流）
func (e *Engine) publishTaskEvent(t *Task, kind string) {
	_ = kind
	_ = t
}

// toAssign 构造下发任务结构
func (t *Task) toAssign() protocol.AssignTask {
	shell := t.Shell
	if shell == "" {
		shell = "auto"
	}
	return protocol.AssignTask{
		ID:            t.ID,
		Name:          t.Name,
		Command:       t.Command,
		Cwd:           t.Cwd,
		Env:           t.Env,
		Shell:         shell,
		TimeoutSec:    t.TimeoutSec,
		MaxMemMB:      t.MaxMemMB,
		MaxCPUPercent: t.MaxCPUPercent,
		Priority:      t.Priority,
	}
}

// IngestLogs 接收 Agent 日志并持久化+扇出
func (e *Engine) IngestLogs(taskID string, lines []protocol.LogItem) (int64, error) {
	lastID, err := e.store.AppendLogs(taskID, lines)
	if err == nil && lastID > 0 {
		// 转成带 id 的行推给实时订阅者
		all, _ := e.store.LogsAfter(taskID, lastID-int64(len(lines)), len(lines))
		if len(all) > 0 {
			e.hub.Publish(taskID, all)
		}
	}
	return lastID, err
}

// FinalizeTask 处理任务结果
func (e *Engine) FinalizeTask(nodeID, taskID string, exitCode int, errMsg, reason string, atMs int64, peakMemMB, cpuMs int64) error {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("任务不存在")
	}
	if t.NodeID != "" && t.NodeID != nodeID {
		return fmt.Errorf("任务不属于该节点")
	}
	return e.store.FinalizeResult(t, exitCode, errMsg, reason, atMs, peakMemMB, cpuMs)
}

// LogsAfter 读取日志
func (e *Engine) LogsAfter(taskID string, after int64, limit int) ([]LogLine, error) {
	return e.store.LogsAfter(taskID, after, limit)
}
