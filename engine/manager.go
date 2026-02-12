package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gollama/internal/config"
)

type Engine struct {
	cfg          config.EngineConfig
	readyPattern *regexp.Regexp

	mu       sync.Mutex
	cmd      *exec.Cmd
	exitCh   chan error
	ready    atomic.Bool
	stopping atomic.Bool
}

func New(cfg config.EngineConfig) (*Engine, error) {
	pattern, err := regexp.Compile(cfg.ReadyPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid ready_pattern: %w", err)
	}
	return &Engine{
		cfg:          cfg,
		readyPattern: pattern,
	}, nil
}

func (e *Engine) IsReady() bool {
	return e.ready.Load()
}

func (e *Engine) Start(ctx context.Context) error {
	readyCh := make(chan error, 1)
	go e.run(ctx, readyCh)
	return <-readyCh
}

func (e *Engine) Stop() error {
	e.stopping.Store(true)
	e.mu.Lock()
	cmd := e.cmd
	exitCh := e.exitCh
	e.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if exitCh == nil {
		return nil
	}

	slog.Info("stopping llama-server")
	_ = cmd.Process.Signal(syscall.SIGINT)

	t := time.NewTimer(e.cfg.StopTimeout)
	defer t.Stop()
	select {
	case err := <-exitCh:
		return err
	case <-t.C:
		slog.Warn("llama-server stop timeout, killing")
		_ = cmd.Process.Kill()
		return errors.New("llama-server killed after stop timeout")
	}
}

func (e *Engine) run(ctx context.Context, readyCh chan error) {
	firstStart := true
	for {
		if ctx.Err() != nil {
			if firstStart {
				readyCh <- ctx.Err()
			}
			return
		}

		err := e.startOnce(ctx)
		if firstStart {
			readyCh <- err
			firstStart = false
		}
		if err != nil {
			slog.Error("engine start failed", slog.Any("error", err))
			if ctx.Err() != nil {
				return
			}
			time.Sleep(e.cfg.RestartBackoff)
			continue
		}

		exitErr := <-e.exitCh
		if e.stopping.Load() {
			return
		}
		e.ready.Store(false)
		slog.Warn("llama-server exited, restarting", slog.Any("error", exitErr))
		time.Sleep(e.cfg.RestartBackoff)
	}
}

func (e *Engine) startOnce(ctx context.Context) error {
	e.mu.Lock()
	cmd := exec.CommandContext(ctx, e.cfg.Bin, e.cfg.Args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		e.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		e.mu.Unlock()
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	e.cmd = cmd
	e.exitCh = make(chan error, 1)
	e.ready.Store(false)
	e.mu.Unlock()

	slog.Info("starting llama-server", slog.Any("bin", e.cfg.Bin), slog.Any("args", e.cfg.Args))
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() { e.exitCh <- cmd.Wait() }()

	ready := make(chan struct{})
	onceReady := sync.Once{}

	startLogReader := func(r io.Reader, name string) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			slog.Info("llama-server", slog.Any("stream", name), slog.Any("line", line))
			if e.readyPattern.MatchString(line) {
				onceReady.Do(func() {
					e.ready.Store(true)
					close(ready)
				})
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Error("llama-server log reader error", slog.Any("stream", name), slog.Any("error", err))
		}
	}

	go startLogReader(stdout, "stdout")
	go startLogReader(stderr, "stderr")

	select {
	case <-ready:
		return nil
	case <-time.After(e.cfg.StartTimeout):
		_ = cmd.Process.Kill()
		return fmt.Errorf("llama-server readiness timeout after %s", e.cfg.StartTimeout)
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return ctx.Err()
	}
}

func (e *Engine) waitForExit() error {
	e.mu.Lock()
	exitCh := e.exitCh
	e.mu.Unlock()
	if exitCh == nil {
		return nil
	}
	return <-exitCh
}
