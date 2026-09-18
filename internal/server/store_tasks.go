package server

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"localpool/internal/protocol"
)

// Task 任务记录（DB + API）
type Task struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Command       string            `json:"command"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env,omitempty"`
	NodeID        string            `json:"node_id,omitempty"`
	NodeName      string            `json:"node_name,omitempty"`
	Status        string            `json:"status"`
	Priority      int               `json:"priority"`
	TimeoutSec    int               `json:"timeout_sec"`
	MaxMemMB      int               `json:"max_mem_mb"`
	MaxCPUPercent int               `json:"max_cpu_pct"`
	NeedIdle      bool              `json:"need_idle"`
	NeedIdleSec   int               `json:"need_idle_sec"`
	RetryOf       string            `json:"retry_of,omitempty"`
	RetryCount    int               `json:"retry_count"`
	CreatedAt     int64             `json:"created_at"`
	StartedAt     int64             `json:"started_at,omitempty"`
	FinishedAt    int64             `json:"finished_at,omitempty"`
	ExitCode      *int              `json:"exit_code"`
	Error         string            `json:"error,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	DurationMs    int64             `json:"duration_ms"`
	PeakMemMB     int64             `json:"peak_mem_mb"`
	CPUMs         int64             `json:"cpu_ms"`
	Shell         string            `json:"shell,omitempty"`

	// 派生展示字段
	HumanReason string `json:"-"`
}

const taskCols = `id,name,command,cwd,env,node_id,node_name,status,priority,timeout_sec,
	max_mem_mb,max_cpu_pct,need_idle,need_idle_sec,retry_of,retry_count,created_at,
	started_at,finished_at,exit_code,error,reason,duration_ms,peak_mem_mb,cpu_ms,shell`

func scanTask(row interface{ Scan(dest ...any) error }) (*Task, error) {
	t := &Task{}
	var needIdle int
	var envRaw string
	var exitCode sql.NullInt64
	err := row.Scan(&t.ID, &t.Name, &t.Command, &t.Cwd, &envRaw, &t.NodeID, &t.NodeName,
		&t.Status, &t.Priority, &t.TimeoutSec, &t.MaxMemMB, &t.MaxCPUPercent, &needIdle,
		&t.NeedIdleSec, &t.RetryOf, &t.RetryCount, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
		&exitCode, &t.Error, &t.Reason, &t.DurationMs, &t.PeakMemMB, &t.CPUMs, &t.Shell)
	if err != nil {
		return nil, err
	}
	t.NeedIdle = needIdle == 1
	if exitCode.Valid {
		v := int(exitCode.Int64)
		t.ExitCode = &v
	}
	if envRaw != "" {
		_ = json.Unmarshal([]byte(envRaw), &t.Env)
	}
	return t, nil
}

func marshalEnv(env map[string]string) string {
	if env == nil {
		return "{}"
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// NewTask 基于创建请求构造任务实体（状态=排队）
func NewTask(name, command, cwd string, env map[string]string, priority, timeoutSec, maxMemMB, maxCPUPercent int, needIdle bool, shell string) *Task {
	if name == "" {
		name = truncate(command, 40)
	}
	if timeoutSec <= 0 {
		timeoutSec = 3600
	}
	return &Task{
		ID:            protocol.NewID("t"),
		Name:          name,
		Command:       command,
		Cwd:           cwd,
		Env:           env,
		Status:        protocol.StatusQueued,
		Priority:      priority,
		TimeoutSec:    timeoutSec,
		MaxMemMB:      maxMemMB,
		MaxCPUPercent: maxCPUPercent,
		NeedIdle:      needIdle,
		CreatedAt:     time.Now().UnixMilli(),
		Shell:         shell,
	}
}

// CreateTask 插入任务
func (s *Store) CreateTask(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO tasks (`+taskCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Command, t.Cwd, marshalEnv(t.Env), "", "", t.Status, t.Priority,
		t.TimeoutSec, t.MaxMemMB, t.MaxCPUPercent, b2i(t.NeedIdle), t.NeedIdleSec, t.RetryOf,
		t.RetryCount, t.CreatedAt, t.StartedAt, t.FinishedAt, nullInt(t.ExitCode), t.Error,
		t.Reason, t.DurationMs, t.PeakMemMB, t.CPUMs, t.Shell)
	return err
}

// GetTask 单个任务
func (s *Store) GetTask(id string) (*Task, error) {
	row := s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=?`, id)
	return scanTask(row)
}

// ListTasks 任务列表 + 分页
func (s *Store) ListTasks(status, node, q string, page, size int) (int, []*Task, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	var conds []string
	var args []any
	if status != "" {
		conds = append(conds, "status=?")
		args = append(args, status)
	}
	if node != "" {
		conds = append(conds, "node_id=?")
		args = append(args, node)
	}
	if q != "" {
		conds = append(conds, "(name LIKE ? OR command LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	offset := (page - 1) * size
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, size, offset)...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return 0, nil, err
		}
		out = append(out, t)
	}
	return total, out, rows.Err()
}

// QueryQueuedTasks 返回排队中的任务，按优先级降序、创建时间升序
func (s *Store) QueryQueuedTasks(limit int) ([]*Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE status='queued'
		ORDER BY priority DESC, created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// StartTask 以 CAS 方式将排队任务置为运行中（调度分配时）。
// 仅在任务仍处于 queued 时生效，避免排队期间被取消/删除的任务被调度器复活执行。
// 返回是否成功抢占（false 表示任务状态已变化，调用方不应再下发）。
func (s *Store) StartTask(id, nodeID, nodeName string, atMs int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE tasks SET status='running', node_id=?, node_name=?, started_at=?, error='', reason='', finished_at=0
		WHERE id=? AND status='queued'`, nodeID, nodeName, atMs, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ClaimRunning 心跳同步：Agent 上报正在运行的任务，若 DB 仍是 queued 则认领为 running
func (s *Store) ClaimRunning(id, nodeID, nodeName string, atMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE tasks SET status='running', node_id=?, node_name=?, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END
		WHERE id=? AND status='queued'`, nodeID, nodeName, atMs, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	_ = n
	return nil
}

// FailTask 将任务置为失败。
// 以 status='running' 作为 CAS 保护：返回是否真正把任务收敛为 failed。
// 避免与用户取消等操作竞态时互相覆盖状态、并错误触发自动重试。
func (s *Store) FailTask(id, reason, errMsg string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(`UPDATE tasks SET status='failed', reason=?, error=?, finished_at=?,
		duration_ms=MAX(duration_ms, ?-started_at) WHERE id=? AND status='running'`,
		reason, errMsg, now, now, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CancelQueued 取消排队中的任务。
// 返回 CAS 是否命中（false 表示任务已被调度器领取/已被他人取消），由调用方决定是否升级处理。
func (s *Store) CancelQueued(id, reason string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE tasks SET status='canceled', reason=?, finished_at=?, duration_ms=0 WHERE id=? AND status='queued'`,
		reason, time.Now().UnixMilli(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CancelRunningTask 将运行中任务置为取消（进程清理由 Agent 侧完成）
func (s *Store) CancelRunningTask(id, reason, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE tasks SET status='canceled', reason=?, error=?, finished_at=? WHERE id=? AND status='running'`,
		reason, errMsg, time.Now().UnixMilli(), id)
	return err
}

// MarkCanceledForNode 将某节点上所有 running 任务标记 canceled(人机避让等)
func (s *Store) MarkCanceledForNode(nodeID, reason, errMsg string) ([]*Task, error) {
	tasks, err := s.QueryRunningOnNode(nodeID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, t := range tasks {
		_, err := s.db.Exec(`UPDATE tasks SET status='canceled', reason=?, error=?, finished_at=? WHERE id=? AND status='running'`,
			reason, errMsg, now, t.ID)
		if err != nil {
			return tasks, err
		}
	}
	return tasks, nil
}

// QueryRunningOnNode 某节点 running 的任务
func (s *Store) QueryRunningOnNode(nodeID string) ([]*Task, error) {
	rows, err := s.db.Query(`SELECT `+taskCols+` FROM tasks WHERE status='running' AND node_id=?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// QueryRunningOnNodeNotIn 查询 running 任务中，不在上报集合里的
func (s *Store) QueryRunningOnNodeNotIn(nodeID string, reported map[string]bool, olderThanMs int64) ([]*Task, error) {
	tasks, err := s.QueryRunningOnNode(nodeID)
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, t := range tasks {
		if reported[t.ID] {
			continue
		}
		// 刚分配的宽限期
		if t.StartedAt > 0 && time.Now().UnixMilli()-t.StartedAt < olderThanMs {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// FinalizeResult 任务结果回写。
// 以 status='running' 作为 CAS 保护：只有任务仍处于 running 时才落终态，
// 防止调度器/用户先置终态（取消、离线失败、超时迁移）后被迟到的 Agent 结果覆盖；
// 若已被收敛为终态，则不改状态、仅把 Agent 实测的统计信息记录上去。
func (s *Store) FinalizeResult(t *Task, exitCode int, errMsg, reason string, atMs int64, peakMemMB, cpuMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := protocol.StatusSuccess
	if exitCode != 0 || errMsg != "" {
		status = protocol.StatusFailed
	}
	dur := int64(0)
	if t.StartedAt > 0 {
		dur = atMs - t.StartedAt
	}
	res, err := s.db.Exec(`UPDATE tasks SET status=?, exit_code=?, error=?, reason=?, finished_at=?,
		duration_ms=?, peak_mem_mb=?, cpu_ms=? WHERE id=? AND status='running'`,
		status, nullInt(ptr(exitCode)), errMsg, reason, atMs, dur, peakMemMB, cpuMs, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	// 状态已被其他路径收敛（failed/canceled）：仅记录执行统计，不再改动状态字段
	_, err = s.db.Exec(`UPDATE tasks SET exit_code=?, error=CASE WHEN error='' THEN ? ELSE error END,
		peak_mem_mb=MAX(peak_mem_mb, ?), cpu_ms=MAX(cpu_ms, ?),
		duration_ms=CASE WHEN duration_ms=0 THEN ?-started_at ELSE duration_ms END
		WHERE id=? AND status IN ('failed','canceled')`,
		nullInt(ptr(exitCode)), errMsg, peakMemMB, cpuMs, atMs, t.ID)
	return err
}

// RetryTask 复制原任务产生一个新的排队任务
func (s *Store) RetryTask(orig *Task) (*Task, error) {
	n := &Task{
		ID:            protocol.NewID("t"),
		Name:          orig.Name,
		Command:       orig.Command,
		Cwd:           orig.Cwd,
		Env:           orig.Env,
		Status:        protocol.StatusQueued,
		Priority:      orig.Priority,
		TimeoutSec:    orig.TimeoutSec,
		MaxMemMB:      orig.MaxMemMB,
		MaxCPUPercent: orig.MaxCPUPercent,
		NeedIdle:      orig.NeedIdle,
		NeedIdleSec:   orig.NeedIdleSec,
		RetryOf:       orig.RetryOf,
		RetryCount:    orig.RetryCount + 1,
		CreatedAt:     time.Now().UnixMilli(),
		Shell:         orig.Shell,
	}
	if n.RetryOf == "" {
		n.RetryOf = orig.ID
	}
	return n, s.CreateTask(n)
}

// DeleteTask 删除任务与日志
func (s *Store) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM task_logs WHERE task_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

// CountTaskByStatus 各状态计数
func (s *Store) CountTaskByStatus(status string) (int, error) {
	var c int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status=?`, status).Scan(&c)
	return c, err
}

// StatsToday 最近 24h 内成功/失败数量
func (s *Store) StatsRange(fromMs int64) (success, failed, canceled int, err error) {
	err = s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='canceled' THEN 1 ELSE 0 END),0)
		FROM tasks WHERE created_at>=?`, fromMs).Scan(&success, &failed, &canceled)
	return
}

// AvgCPUOnline 在线节点平均 CPU
func (s *Store) AvgCPUOnline() (float64, int, error) {
	var avg sql.NullFloat64
	var n int
	err := s.db.QueryRow(`SELECT AVG(cpu_pct), COUNT(*) FROM nodes WHERE online=1`).Scan(&avg, &n)
	return avg.Float64, n, err
}

// TaskRecentHours 最近 N 小时的任务分布（供统计页）
type HourBucket struct {
	Hour    string `json:"hour"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Running int    `json:"running"`
}

func (s *Store) TasksTimeline(hours int) ([]HourBucket, error) {
	// 用 SQLite strftime 聚合
	rows, err := s.db.Query(`SELECT strftime('%Y-%m-%d %H:00', created_at/1000, 'unixepoch', 'localtime') as h,
		SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='running' THEN 1 ELSE 0 END)
		FROM tasks WHERE created_at >= ? GROUP BY h ORDER BY h ASC`,
		time.Now().Add(-time.Duration(hours)*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HourBucket
	for rows.Next() {
		var h HourBucket
		if err := rows.Scan(&h.Hour, &h.Success, &h.Failed, &h.Running); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
