package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jamietre/reveillm/internal/wol"
	"gopkg.in/yaml.v3"
)

type Target struct {
	URL              string        `yaml:"url"`
	APIKey           string        `yaml:"api_key"`
	Timeout          time.Duration `yaml:"timeout"`
	Hook             string        `yaml:"hook"`
	WoL              string        `yaml:"wol"`
	HookTimeout      time.Duration `yaml:"hook_timeout"`
	HookPollInterval time.Duration `yaml:"hook_poll_interval"`
}

type ConfigEntry struct {
	Target string `yaml:"target"`
	Model  string `yaml:"model"`
}

type RouteConfig struct {
	Targets []ConfigEntry `yaml:"targets"`
}

type Config struct {
	Targets map[string]Target      `yaml:"targets"`
	Configs map[string]RouteConfig `yaml:"configs"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	data = interpolateEnv(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	for name, t := range cfg.Targets {
		if (t.Hook != "" || t.WoL != "") && t.HookPollInterval == 0 {
			t.HookPollInterval = 5 * time.Second
		}
		cfg.Targets[name] = t
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

var envVarRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func interpolateEnv(data []byte) []byte {
	return envVarRe.ReplaceAllFunc(data, func(match []byte) []byte {
		return []byte(os.Getenv(string(match[2 : len(match)-1])))
	})
}

func validate(cfg *Config) error {
	for name, t := range cfg.Targets {
		if t.URL == "" {
			return fmt.Errorf("target %q: url is required", name)
		}
		if t.Hook != "" && t.WoL != "" {
			return fmt.Errorf("target %q: hook and wol are mutually exclusive", name)
		}
		if t.WoL != "" {
			if _, err := wol.BuildMagicPacket(t.WoL); err != nil {
				return fmt.Errorf("target %q: invalid wol mac: %w", name, err)
			}
		}
	}
	for cfgName, route := range cfg.Configs {
		for _, entry := range route.Targets {
			if _, ok := cfg.Targets[entry.Target]; !ok {
				return fmt.Errorf("config %q references unknown target %q", cfgName, entry.Target)
			}
		}
	}
	return nil
}
