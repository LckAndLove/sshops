package config

import (
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DefaultUser    string `yaml:"default_user"`
	DefaultPort    int    `yaml:"default_port"`
	DefaultKeyPath string `yaml:"default_key_path"`
	ConnectTimeout int    `yaml:"connect_timeout"`
}

func Default() *Config {
	return &Config{
		DefaultUser:    "root",
		DefaultPort:    22,
		DefaultKeyPath: defaultKeyPath(),
		ConnectTimeout: 30,
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.DefaultUser == "" {
		cfg.DefaultUser = "root"
	}
	if cfg.DefaultPort <= 0 {
		cfg.DefaultPort = 22
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 30
	}
	if cfg.DefaultKeyPath == "" {
		cfg.DefaultKeyPath = defaultKeyPath()
	}

	return cfg, nil
}

func (c *Config) GetDefaultUser() string {
	return c.DefaultUser
}

func (c *Config) GetDefaultPort() int {
	return c.DefaultPort
}

func (c *Config) GetDefaultKeyPath() string {
	return c.DefaultKeyPath
}

func (c *Config) GetConnectTimeout() int {
	return c.ConnectTimeout
}

func defaultKeyPath() string {
	if runtime.GOOS == "windows" {
		userProfile := os.Getenv("USERPROFILE")
		if userProfile == "" {
			userProfile = "."
		}
		return filepath.Join(userProfile, ".ssh", "id_rsa")
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".ssh", "id_rsa")
	}
	return filepath.Join(home, ".ssh", "id_rsa")
}
