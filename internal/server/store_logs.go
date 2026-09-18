package server

import (
	"time"

	"localpool/internal/protocol"
)

// LogLine 日志行（带数据库自增 id，可用于游标）
type LogLine struct {
	ID   int64  `json:"id"`
	Task string `json:"task_id"`
	T    int64  `json:"t"`
	S    string `json:"s"`
	L    string `json:"l"`
}

// AppendLogs 追加一批日志，返回最后一条的自增 id
func (s *Store) AppendLogs(taskID string, lines []protocol.LogItem) (int64, error) {
	if len(lines) == 0 {
		return s.LastLogID(taskID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT INTO task_logs (task_id, ts, stream, line) VALUES (?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	for _, it := range lines {
		stream := "out"
		if it.S == "err" {
			stream = "err"
		}
		ts := it.T
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}
		if _, err := stmt.Exec(taskID, ts, stream, it.L); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return s.lastLogIDLocked(taskID)
}

// LastLogID 最新日志 id
func (s *Store) LastLogID(taskID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastLogIDLocked(taskID)
}

func (s *Store) lastLogIDLocked(taskID string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM task_logs WHERE task_id=?`, taskID).Scan(&id)
	return id, err
}

// LogsAfter 读取 after 之后至多 limit 条日志
func (s *Store) LogsAfter(taskID string, afterID int64, limit int) ([]LogLine, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT id, task_id, ts, stream, line FROM task_logs
		WHERE task_id=? AND id>? ORDER BY id ASC LIMIT ?`, taskID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogLine{}
	for rows.Next() {
		var ll LogLine
		if err := rows.Scan(&ll.ID, &ll.Task, &ll.T, &ll.S, &ll.L); err != nil {
			return nil, err
		}
		out = append(out, ll)
	}
	return out, rows.Err()
}

// LogTotal 日志行数
func (s *Store) LogTotal(taskID string) (int, error) {
	var c int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM task_logs WHERE task_id=?`, taskID).Scan(&c)
	return c, err
}

// LogsTail 取最新 N 条日志（升序返回）
func (s *Store) LogsTail(taskID string, n int) ([]LogLine, int64, error) {
	if n <= 0 || n > 5000 {
		n = 1000
	}
	rows, err := s.db.Query(`SELECT id, task_id, ts, stream, line FROM task_logs
		WHERE task_id=? ORDER BY id DESC LIMIT ?`, taskID, n)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var rev []LogLine
	for rows.Next() {
		var ll LogLine
		if err := rows.Scan(&ll.ID, &ll.Task, &ll.T, &ll.S, &ll.L); err != nil {
			return nil, 0, err
		}
		rev = append(rev, ll)
	}
	// 反转为升序
	out := make([]LogLine, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	last := int64(0)
	if len(rev) > 0 {
		last = rev[0].ID
	}
	return out, last, rows.Err()
}
