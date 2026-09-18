package server

import "encoding/json"

// Settings 全局运行设置（持久化在 kv 表）
type Settings struct {
	// 人机共存策略
	HumanEnabled          bool `json:"human_enabled"`             // 总开关
	EvictOnHumanActive    bool `json:"evict_on_human_active"`     // 运行中的 need_idle 任务在节点被占用时是否终止
	NodeIdleThresholdSec  int  `json:"node_idle_threshold_sec"`   // 距离最近一次人机操作多久算"空闲"，分发给 Agent
	// 调度参数
	HeartbeatMs       int `json:"heartbeat_ms"`        // 期望 Agent 心跳间隔
	OfflineGraceMs    int `json:"offline_grace_ms"`    // 心跳超时多少毫秒判离线
	MaxTasksPerNode   int `json:"max_tasks_per_node"`  // 单节点最大并发任务
	MaxRetries        int `json:"max_retries"`         // 节点丢失后自动重试次数(0=不自动重试)
	DispatchIntervalMs int `json:"dispatch_interval_ms"` // 调度器扫描间隔
	// 默认任务参数（创建任务时可覆盖）
	DefaultTimeoutSec int `json:"default_timeout_sec"`
	// Web
	WebTitle string `json:"web_title"`
}

// DefaultSettings 返回默认设置
func DefaultSettings() Settings {
	return Settings{
		HumanEnabled:          true,
		EvictOnHumanActive:    true,
		NodeIdleThresholdSec:  300, // 5 分钟内无键鼠操作视为空闲
		HeartbeatMs:           2000,
		OfflineGraceMs:        8000,
		MaxTasksPerNode:       2,
		MaxRetries:            0,
		DispatchIntervalMs:    1000,
		DefaultTimeoutSec:     3600,
		WebTitle:              "LocalPool 闲置算力编排平台",
	}
}

// Serialize 将设置序列化为 JSON
func (s *Settings) Serialize() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ParseSettings 解析设置 JSON
func ParseSettings(raw string) Settings {
	s := DefaultSettings()
	if raw == "" {
		return s
	}
	_ = json.Unmarshal([]byte(raw), &s)
	// 防御非法值
	if s.NodeIdleThresholdSec < 10 {
		s.NodeIdleThresholdSec = 10
	}
	if s.HeartbeatMs < 500 {
		s.HeartbeatMs = 500
	}
	if s.OfflineGraceMs < 3000 {
		s.OfflineGraceMs = 3000
	}
	if s.MaxTasksPerNode < 1 {
		s.MaxTasksPerNode = 1
	}
	if s.MaxRetries < 0 {
		s.MaxRetries = 0
	}
	if s.DispatchIntervalMs < 200 {
		s.DispatchIntervalMs = 200
	}
	if s.DefaultTimeoutSec <= 0 {
		s.DefaultTimeoutSec = 3600
	}
	return s
}
