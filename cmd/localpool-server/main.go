// localpool-server 调度中心：任务调度 + 节点管理 + Web/API 服务
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"localpool/internal/server"
	"localpool/internal/webui"
)

var (
	addr   = flag.String("addr", ":8080", "HTTP 监听地址，默认 :8080（监听所有网卡，供局域网 Agent 连接）")
	data   = flag.String("data", "./data", "数据目录（SQLite 持久化位置）")
	dev    = flag.Bool("dev", false, "仅启动 API(不加载内嵌前端)，配合 vite dev 使用")
	secret = flag.String("secret", "", "访问密钥：所有 /api 与 /agent 请求须携带相同密钥（Agent 用 -secret，Web 控制台登录框输入）。留空=不鉴权，仅建议在可信局域网使用")
)

func main() {
	flag.Parse()
	fmt.Println("LocalPool Server 局域网闲置算力调度中心")
	fmt.Printf("  监听地址: %s\n", *addr)
	fmt.Printf("  数据目录: %s\n", *data)

	store, err := server.OpenStore(*data)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	hub := server.NewLogHub()
	eng := server.NewEngine(store, hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	api := server.NewAPIServer(eng, *secret)

	var staticFS fs.FS
	if !*dev {
		staticFS = webui.FS
	}
	handler := api.BuildRouter(staticFS)
	srv := &http.Server{Addr: *addr, Handler: handler}

	if *secret == "" {
		fmt.Println("[!] 未设置 -secret：/api 与 /agent 端点将不做鉴权，请仅在可信局域网使用")
	} else {
		fmt.Println("[i] 已启用访问密钥鉴权（Agent 启动时请加 -secret，Web 控制台会提示输入密钥）")
	}

	// 优雅退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("\n正在优雅关闭...")
		cancel()
		shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shCancel()
		_ = srv.Shutdown(shCtx)
	}()

	fmt.Println("LocalPool Web 控制台: http://localhost" + *addr + "/")
	fmt.Println("（Ctrl+C 退出）")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP 服务异常退出: %v", err)
	}
}
