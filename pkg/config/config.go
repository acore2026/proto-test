package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"mock5g/pkg/transport"
)

type Config struct {
	Transport transport.Config `yaml:"transport"`
	Run       RunConfig        `yaml:"run"`
}

type RunConfig struct {
	Mode          string        `yaml:"mode"`
	Duration      time.Duration `yaml:"duration"`
	Warmup        time.Duration `yaml:"warmup"`
	StepDuration  time.Duration `yaml:"step_duration"`
	StepPPS       int           `yaml:"step_pps"`
	StepCount     int           `yaml:"step_count"`
	BasePPS       int           `yaml:"base_pps"`
	PPS           int           `yaml:"pps"`
	Workers       int           `yaml:"workers"`
	ChannelCount  int           `yaml:"channel_count"`
	OutputCSV     string        `yaml:"output_csv"`
	NASPath       string        `yaml:"nas_template"`
	NASHex        bool          `yaml:"nas_hex"`
	RecvTimeout   time.Duration `yaml:"recv_timeout"`
	ConnectWait   time.Duration `yaml:"connect_wait"`
	StepStartPPS  int           `yaml:"step_start_pps"`
	StepIncrement int           `yaml:"step_increment"`
}

func Default() Config {
	return Config{
		Transport: transport.Config{
			Type:           transport.TypeSCTP,
			LocalIP:        "127.0.0.1",
			LocalPort:      38412,
			RemoteIP:       "127.0.0.1",
			RemotePort:     38412,
			ChannelCount:   1,
			NoDelay:        true,
			HeartbeatMS:    30000,
			ConnectTimeout: 5 * time.Second,
		},
		Run: RunConfig{
			Mode:          "latency",
			Duration:      10 * time.Second,
			Warmup:        2 * time.Second,
			StepDuration:  5 * time.Second,
			StepPPS:       1000,
			StepCount:     5,
			BasePPS:       1000,
			PPS:           1000,
			Workers:       1,
			ChannelCount:  1,
			OutputCSV:     "summary.csv",
			RecvTimeout:   1 * time.Second,
			ConnectWait:   5 * time.Second,
			StepStartPPS:  1000,
			StepIncrement: 1000,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
