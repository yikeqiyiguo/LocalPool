package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"localpool/internal/protocol"
)

// handleAgentRegister Agent 注册/重连
func (a *APIServer) handleAgentRegister(c *gin.Context) {
	var req protocol.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	if req.Name == "" {
		req.Name = req.Host
	}
	if req.ID == "" {
		req.ID = protocol.NewID("n")
	}
	n := &Node{
		ID:           req.ID,
		Name:         req.Name,
		Host:         req.Host,
		OS:           req.OS,
		Arch:         req.Arch,
		CPUModel:     req.CPUModel,
		Cores:        req.Cores,
		MemTotalMB:   req.MemTotalMB,
		AgentVersion: req.AgentVersion,
	}
	if err := a.eng.store.UpsertNode(n); err != nil {
		failMsg(c, "注册失败: "+err.Error())
		return
	}
	st := a.eng.Settings()
	ok(c, protocol.RegisterResp{
		NodeID:                req.ID,
		HeartbeatMs:           st.HeartbeatMs,
		HumanIdleThresholdSec: st.NodeIdleThresholdSec,
	})
}

// handleAgentHeartbeat 心跳：更新节点状态 + 领取任务/取消指令
func (a *APIServer) handleAgentHeartbeat(c *gin.Context) {
	var req protocol.HeartbeatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	exists, _ := a.eng.store.ExistsNode(req.NodeID)
	if !exists {
		fail(c, http.StatusConflict, 10, "node not registered, please re-register")
		return
	}
	if err := a.eng.store.UpdateNodeHeartbeat(req.NodeID, req.Stat, req.HumanActive, req.HumanReason, req.IdleSec); err != nil {
		failMsg(c, err.Error())
		return
	}
	// 先取指令再做任务对账：对账需要知道"本轮已下发"的任务，
	// 避免把刚下发、Agent 尚未开始执行的任务误判为进程丢失（造成重复执行/自动重试）。
	assign, cancels := a.eng.DrainFor(req.NodeID)
	assign = a.eng.PruneAssign(req.NodeID, assign)
	assigned := make(map[string]bool, len(assign))
	for _, at := range assign {
		assigned[at.ID] = true
	}
	nodeName, _ := a.eng.store.GetNodeName(req.NodeID)
	a.eng.ReconcileHeartbeat(req.NodeID, nodeName, req.Running, assigned)

	st := a.eng.Settings()
	ok(c, protocol.HeartbeatResp{
		IntervalMs:            st.HeartbeatMs,
		Assign:                assign,
		Cancel:                cancels,
		HumanIdleThresholdSec: st.NodeIdleThresholdSec,
		ServerTime:            protocol.NowMs(),
	})
}

// handleAgentLogs Agent 批量回传日志
func (a *APIServer) handleAgentLogs(c *gin.Context) {
	var req protocol.LogPushReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	if _, err := a.eng.IngestLogs(req.TaskID, req.Lines); err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, gin.H{"received": len(req.Lines)})
}

// handleAgentResult Agent 上报任务终态
func (a *APIServer) handleAgentResult(c *gin.Context) {
	var req protocol.TaskResultReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数解析失败: "+err.Error())
		return
	}
	at := req.FinishedAtMs
	if at == 0 {
		at = protocol.NowMs()
	}
	if err := a.eng.FinalizeTask(req.NodeID, req.TaskID, req.ExitCode, req.Error, req.Reason,
		at, int64(req.PeakMemMB), int64(req.CPUMs)); err != nil {
		failMsg(c, err.Error())
		return
	}
	ok(c, gin.H{"task_id": req.TaskID})
}
