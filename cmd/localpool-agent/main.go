// localpool-agent 节点 Agent：上报资源、执行任务、回传日志
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"localpool/internal/agent"
)

var (
	server = flag.String("server", "http://127.0.0.1:8080", "调度中心地址，局域网部署时填 Server 机器 IP，如 http://192.168.1.10:8080")
	name   = flag.String("name", "", "节点显示名（默认使用主机名）")
	data   = flag.String("data", "./agent-data", "Agent 本地数据目录（持久化 node_id）")
	secret = flag.String("secret", "", "访问密钥，须与调度中心 localpool-server -secret 一致（可留空表示不鉴权）")
)

func main() {
	flag.Parse()

	cfg, err := agent.LoadConfig(*data, *server, *name)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if *secret != "" {
		cfg.Secret = *secret
		_ = cfg.Save()
	}
	mi := agent.ProbeMachine()
	if cfg.Name == "" {
		cfg.Name = mi.Host
	}

	fmt.Printf("LocalPool Agent v%s\n", agent.Version)
	fmt.Printf("  主机: %s (%s/%s, %d 核, %s)\n", mi.Host, mi.OS, mi.Arch, mi.Cores, mi.CPUModel)
	fmt.Printf("  调度中心: %s\n", cfg.Server)
	fmt.Println("（Ctrl+C 退出）")

	a := agent.NewAgent(cfg, mi)
	a.AttachClient(cfg.Server, cfg.Secret)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n正在退出 Agent...")
		cancel()
	}()

	if err := a.Run(ctx); err != nil {
		log.Printf("Agent 退出: %v", err)
		os.Exit(1)
	}
}
