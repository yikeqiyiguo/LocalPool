package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"localpool/internal/protocol"
)

// handleOverview 全网算力总览
func (a *APIServer) handleOverview(c *gin.Context) {
	st := a.eng.Settings()
	now := time.Now().UnixMilli()
	nodes, err := a.eng.store.ListNodes(now, int64(st.OfflineGraceMs))
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	runningByNode, _ := a.eng.store.CountRunningByNode()

	coresOnline := 0
	var memTotal, memUsed uint64
	online := 0
	var cpuSum float64
	humanActive := []gin.H{}
	for _, n := range nodes {
		if !n.Online {
			continue
		}
		online++
		coresOnline += n.Cores
		memTotal += n.MemTotalMB
		memUsed += n.MemUsedMB
		cpuSum += n.CPUPercent
		if n.HumanActive {
			humanActive = append(humanActive, gin.H{
				"id": n.ID, "name": n.Name, "reason": n.HumanReason,
				"idle_sec": n.IdleSec, "cpu_pct": n.CPUPercent,
			})
		}
	}
	cpuAvg := 0.0
	if online > 0 {
		cpuAvg = cpuSum / float64(online)
	}
	count := func(status string) int {
		v, _ := a.eng.store.CountTaskByStatus(status)
		return v
	}
	todaySuccess, todayFailed, todayCanceled, _ := a.eng.store.StatsRange(now - 24*3600*1000)
	// 正在运行的任务按节点汇总
	runByNode := []gin.H{}
	names := map[string]string{}
	for _, n := range nodes {
		names[n.ID] = n.Name
	}
	for id, cnt := range runningByNode {
		runByNode = append(runByNode, gin.H{"node_id": id, "node_name": names[id], "running": cnt})
	}
	ok(c, gin.H{
		"nodes_total":     len(nodes),
		"nodes_online":    online,
		"cores_online":    coresOnline,
		"cpu_avg":         round1(cpuAvg),
		"mem_total_mb":    memTotal,
		"mem_used_mb":     memUsed,
		"human_active":    humanActive,
		"running_by_node": runByNode,
		"tasks": gin.H{
			"queued": count(protocol.StatusQueued), "running": count(protocol.StatusRunning),
			"success": count(protocol.StatusSuccess), "failed": count(protocol.StatusFailed),
			"canceled": count(protocol.StatusCanceled),
		},
		"today": gin.H{
			"success": todaySuccess, "failed": todayFailed, "canceled": todayCanceled,
		},
	})
}

// handleNodes 节点列表
func (a *APIServer) handleNodes(c *gin.Context) {
	st := a.eng.Settings()
	now := time.Now().UnixMilli()
	nodes, err := a.eng.store.ListNodes(now, int64(st.OfflineGraceMs))
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	runningByNode, _ := a.eng.store.CountRunningByNode()
	for _, n := range nodes {
		n.RunningTasks = runningByNode[n.ID]
	}
	ok(c, nodes)
}

func (a *APIServer) handleNode(c *gin.Context) {
	st := a.eng.Settings()
	n, err := a.eng.store.GetNode(c.Param("id"), time.Now().UnixMilli(), int64(st.OfflineGraceMs))
	if err != nil {
		fail(c, http.StatusNotFound, 404, "节点不存在")
		return
	}
	m, _ := a.eng.store.CountRunningByNode()
	n.RunningTasks = m[n.ID]
	ok(c, n)
}

// handleListTasks 任务列表
func (a *APIServer) handleListTasks(c *gin.Context) {
	status := c.Query("status")
	node := c.Query("node")
	q := strings.TrimSpace(c.Query("q"))
	page := queryInt(c, "page", 1)
	size := queryInt(c, "size", 20)
	total, list, err := a.eng.store.ListTasks(status, node, q, page, size)
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, gin.H{"total": total, "list": list, "page": page, "size": size})
}

func (a *APIServer) handleTaskSummary(c *gin.Context) {
	count := func(status string) int {
		v, _ := a.eng.store.CountTaskByStatus(status)
		return v
	}
	ok(c, gin.H{
		"queued": count(protocol.StatusQueued), "running": count(protocol.StatusRunning),
		"success": count(protocol.StatusSuccess), "failed": count(protocol.StatusFailed),
		"canceled": count(protocol.StatusCanceled),
	})
}

func (a *APIServer) handleGetTask(c *gin.Context) {
	t, err := a.eng.store.GetTask(c.Param("id"))
	if err != nil {
		fail(c, http.StatusNotFound, 404, "任务不存在")
		return
	}
	logTotal, _ := a.eng.store.LogTotal(t.ID)
	ok(c, gin.H{"task": t, "log_lines": logTotal})
}

// createTaskReq 创建任务请求
type createTaskReq struct {
	Name          string            `json:"name"`
	Command       string            `json:"command"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env"`
	Priority      int               `json:"priority"`
	TimeoutSec    int               `json:"timeout_sec"`
	MaxMemMB      int               `json:"max_mem_mb"`
	MaxCPUPercent int               `json:"max_cpu_pct"`
	NeedIdle      *bool             `json:"need_idle"`
	Shell         string            `json:"shell"`
}

func (a *APIServer) handleCreateTask(c *gin.Context) {
	var req createTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		failMsg(c, "命令不能为空")
		return
	}
	needIdle := true
	if req.NeedIdle != nil {
		needIdle = *req.NeedIdle
	}
	st := a.eng.Settings()
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = st.DefaultTimeoutSec
	}
	t := NewTask(req.Name, req.Command, req.Cwd, req.Env, req.Priority,
		req.TimeoutSec, req.MaxMemMB, req.MaxCPUPercent, needIdle, req.Shell)
	if err := a.eng.store.CreateTask(t); err != nil {
		failMsg(c, "创建失败: "+err.Error())
		return
	}
	a.eng.publishTaskEvent(t, "created")
	ok(c, t)
}

func (a *APIServer) handleCancelTask(c *gin.Context) {
	id := c.Param("id")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Reason == "" {
		body.Reason = protocol.ReasonUserCancel
	}
	if err := a.eng.CancelTask(id, body.Reason); err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func (a *APIServer) handleRetryTask(c *gin.Context) {
	nt, err := a.eng.RetryTask(c.Param("id"))
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, nt)
}

func (a *APIServer) handleDeleteTask(c *gin.Context) {
	id := c.Param("id")
	t, err := a.eng.store.GetTask(id)
	if err != nil {
		fail(c, http.StatusNotFound, 404, "任务不存在")
		return
	}
	// 运行中的任务一旦被删除，Agent 上的进程将失去 DB 依据，
	// 心跳对账无法再对其收敛，必须先取消。
	if t.Status == protocol.StatusRunning {
		failMsg(c, "任务运行中，请先取消后再删除")
		return
	}
	if err := a.eng.store.DeleteTask(id); err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// handleTaskLogs 读取任务日志（tail 取最新 N 条，或 after 游标向后翻页）
func (a *APIServer) handleTaskLogs(c *gin.Context) {
	id := c.Param("id")
	tail := queryInt(c, "tail", 0)
	if tail > 0 {
		lines, last, err := a.eng.store.LogsTail(id, tail)
		if err != nil {
			failMsg(c, err.Error())
			return
		}
		total, _ := a.eng.store.LogTotal(id)
		ok(c, gin.H{"lines": lines, "last_id": last, "total": total})
		return
	}
	after := int64(queryInt(c, "after", 0))
	limit := queryInt(c, "limit", 1000)
	lines, err := a.eng.LogsAfter(id, after, limit)
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	last, _ := a.eng.store.LastLogID(id)
	total, _ := a.eng.store.LogTotal(id)
	ok(c, gin.H{"lines": lines, "last_id": last, "total": total})
}

// handleTaskLogStream SSE 实时日志流
func (a *APIServer) handleTaskLogStream(c *gin.Context) {
	id := c.Param("id")
	after := int64(queryInt(c, "after", 0))
	sseHeaders(c)
	// 先补发历史
	backlog, _ := a.eng.LogsAfter(id, after, 2000)
	for _, ll := range backlog {
		b, _ := jsonMarshal(ll)
		writeSSE(c, "line", b)
	}
	last := int64(0)
	if len(backlog) > 0 {
		last = backlog[len(backlog)-1].ID
	}
	// 任务终态通知
	if t, err := a.eng.store.GetTask(id); err == nil && isTerminal(t.Status) {
		b, _ := jsonMarshal(gin.H{"status": t.Status})
		writeSSE(c, "done", b)
		return
	}
	ch := a.hub.Subscribe(id)
	defer a.hub.Unsubscribe(id, ch)
	// 订阅后补一次 DB 查询：覆盖"上方历史查询 ~ Subscribe"窗口内已落库但未扇出的日志
	// （Subscribe 之后的写入都会经 ch 推送，因此只差这一个窗口）。
	if fill, _ := a.eng.LogsAfter(id, last, 2000); len(fill) > 0 {
		for _, ll := range fill {
			if ll.ID > last {
				last = ll.ID
				b, _ := jsonMarshal(ll)
				writeSSE(c, "line", b)
			}
		}
	}
	hb := time.NewTicker(20 * time.Second)
	defer hb.Stop()
	// 周期性检查任务终态：任务结束时通常没有新日志可推，
	// 依赖轮询补发 done 事件，避免 SSE 连接悬空不收敛。
	probe := time.NewTicker(2 * time.Second)
	defer probe.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-probe.C:
			if t, err := a.eng.store.GetTask(id); err == nil && isTerminal(t.Status) {
				// 先尽力冲刷通道内积压行再发 done，保证日志行先于终态事件到达
				for {
					select {
					case ll := <-ch:
						if ll.ID > last {
							last = ll.ID
							b, _ := jsonMarshal(ll)
							writeSSE(c, "line", b)
						}
					default:
						b, _ := jsonMarshal(gin.H{"status": t.Status})
						writeSSE(c, "done", b)
						return
					}
				}
			}
		case ll := <-ch:
			if ll.ID <= last {
				continue
			}
			last = ll.ID
			b, _ := jsonMarshal(ll)
			writeSSE(c, "line", b)
		case <-hb.C:
			_, _ = c.Writer.WriteString(": ping\n\n")
			c.Writer.Flush()
		}
	}
}

// handleTimeline 历史任务统计
func (a *APIServer) handleTimeline(c *gin.Context) {
	hours := queryInt(c, "hours", 24)
	if hours < 1 || hours > 720 {
		hours = 24
	}
	buckets, err := a.eng.store.TasksTimeline(hours)
	if err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, buckets)
}

func (a *APIServer) handleGetConfig(c *gin.Context) {
	ok(c, a.eng.Settings())
}

func (a *APIServer) handlePutConfig(c *gin.Context) {
	var req Settings
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	if err := a.eng.UpdateSettings(req); err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, a.eng.Settings())
}

func isTerminal(status string) bool {
	return status == protocol.StatusSuccess || status == protocol.StatusFailed || status == protocol.StatusCanceled
}

func round1(f float64) float64 {
	return float64(int(f*10)) / 10
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
