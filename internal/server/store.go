package server

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Store SQLite 持久化层。
// MVP 采用纯 Go 的 SQLite(modernc.org/sqlite)，零 CGO 依赖。
type Store struct {
	db  *sql.DB
	mu  sync.Mutex // 串行化写入，避免 SQLITE_BUSY
	dir string
}

// OpenStore 打开（必要时创建）数据目录与数据库
func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "localpool.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	// WAL + busy_timeout 保证读写并发下稳定
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", pragma, err)
		}
	}
	s := &Store{db: db, dir: dataDir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT,
			os TEXT,
			arch TEXT,
			cpu_model TEXT,
			cpu_cores INTEGER DEFAULT 0,
			mem_total_mb INTEGER DEFAULT 0,
			agent_version TEXT,
			first_seen INTEGER DEFAULT 0,
			last_seen INTEGER DEFAULT 0,
			online INTEGER DEFAULT 0,
			cpu_pct REAL DEFAULT 0,
			mem_used_mb INTEGER DEFAULT 0,
			disk_total_mb INTEGER DEFAULT 0,
			disk_used_mb INTEGER DEFAULT 0,
			load1 REAL DEFAULT 0,
			net_in_bps INTEGER DEFAULT 0,
			net_out_bps INTEGER DEFAULT 0,
			uptime_sec INTEGER DEFAULT 0,
			human_active INTEGER DEFAULT 0,
			human_reason TEXT DEFAULT '',
			idle_sec REAL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_online ON nodes(online)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			name TEXT,
			command TEXT NOT NULL,
			cwd TEXT DEFAULT '',
			env TEXT DEFAULT '{}',
			node_id TEXT,
			node_name TEXT,
			status TEXT NOT NULL,
			priority INTEGER DEFAULT 0,
			timeout_sec INTEGER DEFAULT 3600,
			max_mem_mb INTEGER DEFAULT 0,
			max_cpu_pct INTEGER DEFAULT 0,
			need_idle INTEGER DEFAULT 1,
			need_idle_sec INTEGER DEFAULT 0,
			retry_of TEXT,
			retry_count INTEGER DEFAULT 0,
			created_at INTEGER DEFAULT 0,
			started_at INTEGER DEFAULT 0,
			finished_at INTEGER DEFAULT 0,
			exit_code INTEGER,
			error TEXT,
			reason TEXT,
			duration_ms INTEGER DEFAULT 0,
			peak_mem_mb INTEGER DEFAULT 0,
			cpu_ms INTEGER DEFAULT 0,
			shell TEXT DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_node ON tasks(node_id)`,
		`CREATE TABLE IF NOT EXISTS task_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			ts INTEGER DEFAULT 0,
			stream TEXT DEFAULT 'out',
			line TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_task ON task_logs(task_id, id)`,
		`CREATE TABLE IF NOT EXISTS kv (
			k TEXT PRIMARY KEY,
			v TEXT
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return fmt.Errorf("migrate: %w\nstmt: %s", err, st)
		}
	}
	return nil
}
