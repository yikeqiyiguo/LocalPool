package server

import "sync"

// LogHub 按任务进行内存日志扇出，支撑 SSE 秒级实时推送
type LogHub struct {
	mu     sync.RWMutex
	topics map[string]map[chan LogLine]struct{}
}

// NewLogHub 创建
func NewLogHub() *LogHub {
	return &LogHub{topics: map[string]map[chan LogLine]struct{}{}}
}

// Subscribe 订阅某任务的新日志
func (h *LogHub) Subscribe(taskID string) chan LogLine {
	ch := make(chan LogLine, 1024)
	h.mu.Lock()
	if h.topics[taskID] == nil {
		h.topics[taskID] = map[chan LogLine]struct{}{}
	}
	h.topics[taskID][ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe 退订
func (h *LogHub) Unsubscribe(taskID string, ch chan LogLine) {
	h.mu.Lock()
	if m, ok := h.topics[taskID]; ok {
		delete(m, ch)
		if len(m) == 0 {
			delete(h.topics, taskID)
		}
	}
	h.mu.Unlock()
}

// Publish 推送给订阅者（非阻塞）
func (h *LogHub) Publish(taskID string, lines []LogLine) {
	if len(lines) == 0 {
		return
	}
	h.mu.RLock()
	subs := h.topics[taskID]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	// 拷贝订阅列表，避免持锁写通道阻塞
	list := make([]chan LogLine, 0, len(subs))
	for c := range subs {
		list = append(list, c)
	}
	h.mu.RUnlock()
	for _, ll := range lines {
		for _, ch := range list {
			select {
			case ch <- ll:
			default: // 订阅端慢则丢弃，避免阻塞日志写入
			}
		}
	}
}
