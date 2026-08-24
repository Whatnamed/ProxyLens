package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config 存储 Collector 运行配置
type Config struct {
	ControllerURL       string
	ConnectionsInterval int
	Secret              string
	QueueCapacity       int
	InitialBackoffMs    int
	MaxBackoffMs        int
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		ControllerURL:       "http://127.0.0.1:9090",
		ConnectionsInterval: 250,
		Secret:              os.Getenv("MIHOMO_SECRET"),
		QueueCapacity:       200,
		InitialBackoffMs:    500,
		MaxBackoffMs:        10000,
	}
}

// ParseFlags 解析命令行参数并结合环境变量
func ParseFlags(args []string) (*Config, error) {
	cfg := DefaultConfig()

	fs := flag.NewFlagSet("collector", flag.ContinueOnError)
	fs.StringVar(&cfg.ControllerURL, "controller", cfg.ControllerURL, "Mihomo external controller URL")
	fs.IntVar(&cfg.ConnectionsInterval, "connections-interval", cfg.ConnectionsInterval, "Snapshot interval in ms (250, 500, 1000)")
	fs.StringVar(&cfg.Secret, "secret", cfg.Secret, "Controller secret (prefers MIHOMO_SECRET env var)")
	fs.IntVar(&cfg.QueueCapacity, "queue-capacity", cfg.QueueCapacity, "Bounded event queue capacity")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// 校验 Controller URL
	u, err := url.Parse(cfg.ControllerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid controller URL: %s", cfg.ControllerURL)
	}

	if cfg.ConnectionsInterval <= 0 {
		cfg.ConnectionsInterval = 250
	}

	return &cfg, nil
}

// RedactedControllerURL 返回隐藏敏感信息的 Controller URL
func (c *Config) RedactedControllerURL() string {
	u, err := url.Parse(c.ControllerURL)
	if err != nil {
		return "[INVALID URL]"
	}
	if u.User != nil {
		return fmt.Sprintf("%s://[REDACTED]@%s", u.Scheme, u.Host)
	}
	return c.ControllerURL
}

// String 打印安全配置（完全隐藏 Secret）
func (c *Config) String() string {
	secretStatus := "NONE"
	if strings.TrimSpace(c.Secret) != "" {
		secretStatus = "[SET / REDACTED]"
	}
	return fmt.Sprintf("Controller=%s, Interval=%dms, Secret=%s, QueueCapacity=%d",
		c.RedactedControllerURL(), c.ConnectionsInterval, secretStatus, c.QueueCapacity)
}
