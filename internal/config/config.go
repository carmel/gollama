package config

import (
	"os"
	"time"

	logger "github.com/carmel/go-pkg/logger"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Server ServerConfig  `yaml:"server"`
	Engine EngineConfig  `yaml:"engine"`
	Queue  QueueConfig   `yaml:"queue"`
	Logger logger.Option `yaml:"logger"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type EngineConfig struct {
	Bin            string        `yaml:"bin"`
	Args           []string      `yaml:"args"`
	BaseURL        string        `yaml:"base_url"`
	ReadyPattern   string        `yaml:"ready_pattern"`
	StartTimeout   time.Duration `yaml:"start_timeout"`
	StopTimeout    time.Duration `yaml:"stop_timeout"`
	RestartBackoff time.Duration `yaml:"restart_backoff"`
}

type QueueConfig struct {
	MaxConcurrency int           `yaml:"max_concurrency"`
	WaitTimeout    time.Duration `yaml:"wait_timeout"`
}

func Load(path string) (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Addr: ":8081",
		},
		Engine: EngineConfig{
			Bin:            "./bin/llama-server",
			BaseURL:        "http://127.0.0.1:8080",
			ReadyPattern:   "listening|ready|server is running",
			StartTimeout:   30 * time.Second,
			StopTimeout:    10 * time.Second,
			RestartBackoff: 2 * time.Second,
		},
		Queue: QueueConfig{
			MaxConcurrency: 1,
			WaitTimeout:    60 * time.Second,
		},
		Logger: logger.Option{
			LogLevel: "debug",
		},
	}
}
