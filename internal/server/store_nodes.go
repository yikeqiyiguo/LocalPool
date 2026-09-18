package server

import (
	"database/sql"
	"time"

	"localpool/internal/protocol"
)

// Node 节点（DB + API 展示）
type Node struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	CPUModel     string `json:"cpu_model"`
	Cores        int    `json:"cores"`
	MemTotalMB   uint64 `json:"mem_total_mb"`
	AgentVersion string `json:"agent_version"`

	Online       bool    `json:"online"`
	FirstSeen    int64   `json:"first_seen"`
	LastSeen     int64   `json:"last_seen"`
	CPUPercent   float64 `json:"cpu_pct"`
	MemUsedMB    uint64  `json:"mem_used_mb"`
	DiskTotalMB  uint64  `json:"disk_total_mb"`
	DiskUsedMB   uint64  `json:"disk_used_mb"`
	Load1        float64 `json:"load1"`
	NetInBps     uint64  `json:"net_in_bps"`
	NetOutBps    uint64  `json:"net_out_bps"`
	UptimeSec    uint64  `json:"uptime_sec"`
	HumanActive  bool    `json:"human_active"`
	HumanReason  string  `json:"human_reason,omitempty"`
	IdleSec      float64 `json:"idle_sec"`
	RunningTasks int     `json:"running_tasks"`

	// 派生字段
	CPUFreePercent float64 `json:"cpu_free_pct"`
	MemFreeMB      uint64  `json:"mem_free_mb"`
	DiskUsedPct    float64 `json:"disk_used_pct"`
	LastSeenAgoMs  int64   `json:"last_seen_ago_ms"`
}

func scanNode(row interface{ Scan(...any) error }) (*Node, error) {
	var n Node
	var online, humanActive int
	err := row.Scan(&n.ID, &n.Name, &n.Host, &n.OS, &n.Arch, &n.CPUModel,
		&n.Cores, &n.MemTotalMB, &n.AgentVersion, &n.FirstSeen, &n.LastSeen,
		&online, &n.CPUPercent, &n.MemUsedMB, &n.DiskTotalMB, &n.DiskUsedMB,
		&n.Load1, &n.NetInBps, &n.NetOutBps, &n.UptimeSec, &humanActive,
		&n.HumanReason, &n.IdleSec)
	if err != nil {
		return nil, err
	}
	n.Online = online == 1
	n.HumanActive = humanActive == 1
	return &n, nil
}

const nodeCols = `id,name,host,os,arch,cpu_model,cpu_cores,mem_total_mb,agent_version,
	first_seen,last_seen,online,cpu_pct,mem_used_mb,disk_total_mb,disk_used_mb,
	load1,net_in_bps,net_out_bps,uptime_sec,human_active,human_reason,idle_sec`

// UpsertNode 注册节点：插入或复用
func (s *Store) UpsertNode(n *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO nodes (id,name,host,os,arch,cpu_model,cpu_cores,mem_total_mb,agent_version,first_seen,last_seen,online)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,1)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, host=excluded.host, os=excluded.os, arch=excluded.arch,
			cpu_model=excluded.cpu_model, cpu_cores=excluded.cpu_cores,
			mem_total_mb=excluded.mem_total_mb, agent_version=excluded.agent_version,
			last_seen=excluded.last_seen, online=1`,
		n.ID, n.Name, n.Host, n.OS, n.Arch, n.CPUModel, n.Cores, n.MemTotalMB,
		n.AgentVersion, now, now)
	return err
}

// UpdateNodeHeartbeat 心跳时更新节点状态与资源快照
func (s *Store) UpdateNodeHeartbeat(id string, st protocol.MachineStat, humanActive bool, humanReason string, idleSec float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	// 能收到心跳即视为在线；离线由调度器依据 last_seen 超时(deriveNode/MarkNodeOffline)判定
	_, err := s.db.Exec(`UPDATE nodes SET last_seen=?, online=1, cpu_pct=?, mem_used_mb=?,
		disk_total_mb=?, disk_used_mb=?, load1=?, net_in_bps=?, net_out_bps=?, uptime_sec=?,
		human_active=?, human_reason=?, idle_sec=? WHERE id=?`,
		now, st.CPUPercent, st.MemUsedMB, st.DiskTotalMB, st.DiskUsedMB, st.Load1,
		st.NetInBps, st.NetOutBps, st.UptimeSec, b2i(humanActive), humanReason, idleSec, id)
	return err
}

// MarkNodeOffline 标记节点离线
func (s *Store) MarkNodeOffline(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE nodes SET online=0 WHERE id=?`, id)
	return err
}

// ListNodes 返回全部节点并补充派生字段
func (s *Store) ListNodes(now int64, offlineGraceMs int64) ([]*Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY online DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		deriveNode(n, now, offlineGraceMs)
		out = append(out, n)
	}
	return out, rows.Err()
}

// CountRunningByNode 返回每个节点上的 running 任务数
func (s *Store) CountRunningByNode() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT node_id, COUNT(*) FROM tasks WHERE status='running' AND node_id IS NOT NULL GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int{}
	for rows.Next() {
		var id string
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		m[id] = c
	}
	return m, rows.Err()
}

// GetNode 单节点
func (s *Store) GetNode(id string, now int64, offlineGraceMs int64) (*Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE id=?`, id)
	n, err := scanNode(row)
	if err != nil {
		return nil, err
	}
	deriveNode(n, now, offlineGraceMs)
	return n, nil
}

// GetNodeName 查询节点名（心跳对账用）
func (s *Store) GetNodeName(id string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM nodes WHERE id=?`, id).Scan(&name)
	return name, err
}

// ExistsNode 判断节点是否已注册
func (s *Store) ExistsNode(id string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM nodes WHERE id=?`, id).Scan(&one)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func deriveNode(n *Node, now, graceMs int64) {
	freePct := 100.0 - n.CPUPercent
	if freePct < 0 {
		freePct = 0
	}
	n.CPUFreePercent = freePct
	if n.MemTotalMB > n.MemUsedMB {
		n.MemFreeMB = n.MemTotalMB - n.MemUsedMB
	}
	if n.DiskTotalMB > 0 {
		n.DiskUsedPct = float64(n.DiskUsedMB) / float64(n.DiskTotalMB) * 100
	}
	n.LastSeenAgoMs = now - n.LastSeen
	if n.Online && n.LastSeenAgoMs > graceMs {
		n.Online = false
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ = sql.ErrNoRows
