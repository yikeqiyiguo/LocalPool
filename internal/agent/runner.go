package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"localpool/internal/protocol"
)

// runTask 执行单任务完整生命周期：启动、管道、监控、日志、结果
func (a *Agent) runTask(ctx context.Context, ps *procState) {
	t := ps.task
	defer func() {
		a.mu.Lock()
		delete(a.running, t.ID)
		a.mu.Unlock()
	}()

	logCh := make(chan protocol.LogItem, 2048)
	logDone := make(chan struct{})
	var flushWG sync.WaitGroup
	go a.logWriter(t.ID, logCh, logDone, &flushWG)

	pushLog := func(s, line string) {
		select {
		case logCh <- protocol.LogItem{S: s, T: protocol.NowMs(), L: line}:
		default:
		}
	}
	failStart := func(code int, errMsg string) {
		pushLog("err", "[LocalPool] "+errMsg)
		close(logCh)
		<-logDone
		a.sendResult(protocol.TaskResultReq{
			ExitCode: code, Error: errMsg,
			StartedAtMs: ps.startMs, FinishedAtMs: protocol.NowMs(),
		})
	}

	cmd, err := buildTaskCmd(t)
	if err != nil {
		failStart(127, "无法构建命令: "+err.Error())
		return
	}
	ps.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		failStart(127, "StdoutPipe: "+err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		failStart(127, "StderrPipe: "+err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		failStart(127, "启动命令失败: "+err.Error())
		return
	}
	// 与心跳采样 snapshotRunning 读侧加同一把锁，避免 startMs 数据竞态
	ps.mu.Lock()
	ps.startMs = protocol.NowMs()
	ps.mu.Unlock()
	pushLog("out", fmt.Sprintf("[LocalPool] 任务开始执行  pid=%d  命令: %s", cmd.Process.Pid, t.Command))

	// 输出管道读取 goroutine
	var readWG sync.WaitGroup
	readWG.Add(2)
	go func() {
		defer readWG.Done()
		scanLines(stdout, "out", logCh)
	}()
	go func() {
		defer readWG.Done()
		scanLines(stderr, "err", logCh)
	}()

	doneCh := make(chan struct{})
	killCh := make(chan string, 1)
	killReason := func(reason string) {
		select {
		case killCh <- reason:
		default:
		}
	}

	// 资源监控 + 超时/取消终止（运行期间持续工作）
	go a.monitorProc(ps, cmd, doneCh, pushLog, killReason)
	go a.terminator(ps, cmd, t, ctx, doneCh, killReason, pushLog)

	waitErr := cmd.Wait() // 阻塞直到进程退出
	readWG.Wait()
	close(doneCh)

	// 判定结果
	var exitCode int
	var exitMsg string
	reason := ""
	select {
	case reason = <-killCh:
	default:
	}
	if reason != "" {
		exitCode = -1
		exitMsg = reasonDesc(reason)
	} else if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
		exitMsg = waitErr.Error()
	} else {
		exitCode = 0
	}

	ps.mu.Lock()
	cpuNs := ps.lastCpuNs
	peakKB := ps.peakMemKB
	ps.mu.Unlock()

	finished := protocol.NowMs()
	if reason == "" {
		pushLog("out", fmt.Sprintf("[LocalPool] 任务结束  exit=%d", exitCode))
	} else {
		pushLog("err", fmt.Sprintf("[LocalPool] 任务被终止: %s", exitMsg))
	}
	close(logCh)
	<-logDone
	flushWG.Wait()

	a.sendResult(protocol.TaskResultReq{
		ExitCode:     exitCode,
		Error:        exitMsg,
		Reason:       reason,
		StartedAtMs:  ps.startMs,
		FinishedAtMs: finished,
		PeakMemMB:    peakKB / 1024,
		CPUMs:        cpuNs / 1_000_000,
		Killed:       reason != "",
	})
	log.Printf("[LocalPool] 任务 %s 结束 exit=%d elapsed=%ds reason=%s", t.ID, exitCode,
		int((finished-ps.startMs)/1000), reason)
}

func reasonDesc(r string) string {
	switch r {
	case protocol.ReasonTimeout:
		return "任务执行超时，已被 LocalPool 终止"
	case protocol.ReasonMemLimit:
		return "任务内存使用超过限制，已被 LocalPool 终止"
	case protocol.ReasonUserCancel:
		return "任务已取消(调度中心/用户)"
	default:
		return "任务被终止(" + r + ")"
	}
}

// scanLines 逐行读取流并写入日志通道
func scanLines(r interface{ Read([]byte) (int, error) }, stream string, ch chan<- protocol.LogItem) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" {
			continue
		}
		select {
		case ch <- protocol.LogItem{S: stream, T: protocol.NowMs(), L: line}:
		default:
		}
	}
}

// logWriter 日志批量回传：每 300ms 或满 200 行 flush 一次
func (a *Agent) logWriter(taskID string, ch <-chan protocol.LogItem, done chan<- struct{}, wg *sync.WaitGroup) {
	buf := make([]protocol.LogItem, 0, 256)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		lines := buf
		buf = make([]protocol.LogItem, 0, 256)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := a.cli.postJSON(ctx, "/agent/logs", protocol.LogPushReq{
				NodeID: a.currentNodeID(), TaskID: taskID, Lines: lines,
			}, nil); err != nil {
				log.Printf("[LocalPool] 日志回传失败(%d 行): %v", len(lines), err)
			}
		}()
	}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case it, ok := <-ch:
			if !ok {
				flush()
				close(done)
				return
			}
			buf = append(buf, it)
			if len(buf) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// monitorProc 周期采样进程资源；超限执行软限制
func (a *Agent) monitorProc(ps *procState, cmd *exec.Cmd, done <-chan struct{},
	pushLog func(string, string), killReason func(string)) {

	t := ps.task
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastCpu := uint64(0)
	lastAt := time.Now()
	throttled := false
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			cpuNs, rssKB, err := sampleProcTree(cmd)
			if err != nil {
				// 进程可能刚好退出，等待 done
				continue
			}
			now := time.Now()
			d := now.Sub(lastAt).Nanoseconds()
			var pct float64
			if d > 0 && cpuNs >= lastCpu {
				pct = float64(cpuNs-lastCpu) / float64(d) * 100
			}
			lastCpu = cpuNs
			lastAt = now

			ps.mu.Lock()
			ps.lastCpuPct = pct
			ps.lastMemMB = float64(rssKB) / 1024
			ps.lastCpuNs = cpuNs
			if rssKB > ps.peakMemKB {
				ps.peakMemKB = rssKB
			}
			ps.mu.Unlock()

			// 内存软限制（超出即终止）
			if t.MaxMemMB > 0 && rssKB/1024 > uint64(t.MaxMemMB) {
				pushLog("err", fmt.Sprintf("[LocalPool] 内存占用 %.0fMB 超过上限 %dMB，终止任务",
					float64(rssKB)/1024, t.MaxMemMB))
				killReason(protocol.ReasonMemLimit)
				_ = killTaskProc(cmd)
				return
			}
			// CPU 软限制（降优先级 + 提示）
			if t.MaxCPUPercent > 0 && pct > float64(t.MaxCPUPercent) {
				if !throttled {
					throttled = true
					_ = throttleProc(cmd)
					pushLog("warn", fmt.Sprintf("[LocalPool] CPU 占用 %.0f%% 超过上限 %d%%，已自动降低进程优先级",
						pct, t.MaxCPUPercent))
				}
			} else if throttled && pct <= float64(t.MaxCPUPercent) {
				throttled = false
			}
		}
	}
}

// terminator 监听取消/超时/上下文退出并终止任务
func (a *Agent) terminator(ps *procState, cmd *exec.Cmd, t protocol.AssignTask, ctx context.Context,
	done <-chan struct{}, killReason func(string), pushLog func(string, string)) {

	timeout := time.Duration(t.TimeoutSec) * time.Second
	if t.TimeoutSec <= 0 {
		timeout = 24 * time.Hour
	}
	var timerC <-chan time.Time
	timer := time.NewTimer(timeout)
	timerC = timer.C
	defer timer.Stop()

	for {
		select {
		case <-done:
			return
		case <-ps.stop:
			pushLog("err", "[LocalPool] 收到取消/避让指令，正在终止进程树")
			killReason(protocol.ReasonUserCancel)
			_ = killTaskProc(cmd)
			return
		case <-ctx.Done():
			killReason(protocol.ReasonAgentQuit)
			_ = killTaskProc(cmd)
			return
		case <-timerC:
			pushLog("err", fmt.Sprintf("[LocalPool] 任务运行超过 %d 秒，超时终止", t.TimeoutSec))
			killReason(protocol.ReasonTimeout)
			_ = killTaskProc(cmd)
			return
		}
	}
}

// sendResult 回传任务结果
func (a *Agent) sendResult(r protocol.TaskResultReq) {
	r.NodeID = a.currentNodeID()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.cli.postJSON(ctx, "/agent/result", r, nil); err != nil {
		log.Printf("[LocalPool] 结果回传失败 task=%s: %v", r.TaskID, err)
	}
}
