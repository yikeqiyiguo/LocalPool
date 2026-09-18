package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config Agent 本地配置（node_id 持久化，保证重连后仍是同一节点）
type Config struct {
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	Server   string `json:"server"`
	Secret   string `json:"secret,omitempty"`
	DataDir  string `json:"data_dir"`
}

// LoadConfig 加载或初始化配置
func LoadConfig(dataDir, server, name string) (*Config, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dataDir, "agent.json")
	cfg := &Config{Server: server, Name: name, DataDir: dataDir}
	if b, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(b, cfg)
	}
	if cfg.Server == "" {
		cfg.Server = server
	}
	if cfg.Name == "" {
		cfg.Name = name
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.WriteFile(cfgPath, b, 0o644)
	return cfg, nil
}

// Save 持久化（注册后回写 node_id）
func (c *Config) Save() error {
	cfgPath := filepath.Join(c.DataDir, "agent.json")
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, b, 0o644)
}
