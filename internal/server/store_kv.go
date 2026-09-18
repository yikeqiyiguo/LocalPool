package server

import "database/sql"

// GetKv 读取键值
func (s *Store) GetKv(k string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetKv 写入键值
func (s *Store) SetKv(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO kv (k,v) VALUES (?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

const kvSettings = "settings"

// LoadSettings 读取持久化设置，无则用默认
func (s *Store) LoadSettings() Settings {
	raw, _ := s.GetKv(kvSettings)
	return ParseSettings(raw)
}

// SaveSettings 保存设置
func (s *Store) SaveSettings(st Settings) error {
	return s.SetKv(kvSettings, st.Serialize())
}
