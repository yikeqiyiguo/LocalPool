// Package protocol 定义 Server <-> Agent 之间通信的数据结构，
// 以及前后端 API 复用的枚举常量。MVP 采用轻量 HTTP/JSON + SSE 实时流。
package protocol

// 任务状态（与前端约定一致）
const (
	StatusQueued   = "queued"   // 排队中
	StatusRunning  = "running"  // 运行中
	StatusSuccess  = "success"  // 成功
	StatusFailed   = "failed"   // 失败
	StatusCanceled = "canceled" // 已取消
)

// 任务结束/失败原因
const (
	ReasonUserCancel = "user_cancel"     // 用户主动取消
	ReasonTimeout    = "timeout"         // 任务超时
	ReasonNodeLost   = "node_lost"       // 执行节点离线
	ReasonHumanEvict = "human_evict"     // 人机避让：检测到有人使用机器
	ReasonMemLimit   = "mem_limit"       // 超出内存软限制
	ReasonCPUThrottle = "cpu_throttle"   // CPU 软限制(仅降权提示，不终止)
	ReasonAgentQuit  = "agent_quit"      // Agent 进程退出
)

// MachineStat 单次机器资源快照
type MachineStat struct {
	CPUPercent float64 `json:"cpu_pct"` // 总 CPU 使用率 0-100
	MemTotalMB uint64  `json:"mem_total_mb"`
	MemUsedMB  uint64  `json:"mem_used_mb"`
	DiskTotalMB uint64 `json:"disk_total_mb"`
	DiskUsedMB  uint64 `json:"disk_used_mb"`
	Load1       float64 `json:"load1"`
	NetInBps    uint64  `json:"net_in_bps"`
	NetOutBps   uint64  `json:"net_out_bps"`
	UptimeSec   uint64  `json:"uptime_sec"`
}

// RunningTaskStatus Agent 上正在运行的某个任务的最新资源与状态
type RunningTaskStatus struct {
	TaskID      string  `json:"task_id"`
	PID         int     `json:"pid"`
	CPUPercent  float64 `json:"cpu_pct"`
	MemMB       uint64  `json:"mem_mb"`
	ElapsedSec  float64 `json:"elapsed_sec"`
	StartedAtMs int64   `json:"started_at_ms"`
}

// HeartbeatReq 心跳上报
type HeartbeatReq struct {
	NodeID     string              `json:"node_id"`
	Version    string              `json:"version"`
	HumanActive bool               `json:"human_active"`
	HumanReason string             `json:"human_reason,omitempty"`
	IdleSec    float64             `json:"idle_sec"`
	Stat       MachineStat         `json:"stat"`
	Running    []RunningTaskStatus `json:"running,omitempty"`
}

// AssignTask 调度器下发给节点执行的任务
type AssignTask struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Command      string            `json:"command"`
	Cwd          string            `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Shell        string            `json:"shell,omitempty"` // windows=cmd / linux=bash
	TimeoutSec   int               `json:"timeout_sec"`
	MaxMemMB     int               `json:"max_mem_mb"`   // 0 = 不限
	MaxCPUPercent int              `json:"max_cpu_pct"`  // 0 = 不限
	Priority     int               `json:"priority"`
}

// HeartbeatResp 心跳应答：携带新任务与取消指令
type HeartbeatResp struct {
	IntervalMs          int          `json:"interval_ms"`
	Assign              []AssignTask `json:"assign,omitempty"`
	Cancel              []string     `json:"cancel,omitempty"`
	HumanIdleThresholdSec int        `json:"human_idle_threshold_sec"`
	ServerTime          int64        `json:"server_time"`
}

// LogItem 一行日志 s=o(out)/e(err)
type LogItem struct {
	S string `json:"s"` // out / err
	T int64  `json:"t"` // ms 时间戳
	L string `json:"l"` // 行内容
}

// LogPushReq Agent 批量回传任务日志
type LogPushReq struct {
	NodeID string    `json:"node_id"`
	TaskID string    `json:"task_id"`
	Lines  []LogItem `json:"lines"`
}

// TaskResultReq 任务结束结果上报
type TaskResultReq struct {
	NodeID       string `json:"node_id"`
	TaskID       string `json:"task_id"`
	ExitCode     int    `json:"exit_code"`
	Signal       string `json:"signal,omitempty"`
	Error        string `json:"error,omitempty"`
	Reason       string `json:"reason,omitempty"` // 非正常结束时给原因
	StartedAtMs  int64  `json:"started_at_ms"`
	FinishedAtMs int64  `json:"finished_at_ms"`
	PeakMemMB    uint64 `json:"peak_mem_mb"`
	CPUMs        uint64 `json:"cpu_ms"`
	Killed       bool   `json:"killed"`
}

// RegisterReq Agent 注册/重新注册
type RegisterReq struct {
	ID          string `json:"id,omitempty"` // 已有 ID 则复用
	Name        string `json:"name"`
	Host        string `json:"host"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUModel    string `json:"cpu_model"`
	Cores       int    `json:"cores"`
	MemTotalMB  uint64 `json:"mem_total_mb"`
	AgentVersion string `json:"agent_version"`
}

// RegisterResp 注册应答
type RegisterResp struct {
	NodeID        string `json:"node_id"`
	HeartbeatMs   int    `json:"heartbeat_ms"`
	HumanIdleThresholdSec int `json:"human_idle_threshold_sec"`
}
