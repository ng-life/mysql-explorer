package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Cluster keeps credentials private; PasswordEnv overrides the configured password.
type Cluster struct {
	Type        string `toml:"type"`
	AllowDML    bool   `toml:"allow_dml"`
	AllowDDL    bool   `toml:"allow_ddl"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Address     string `toml:"address"`
	User        string `toml:"user"`
	Password    string `toml:"password"`
	PasswordEnv string `toml:"password_env"`
}
type Config struct {
	Listen   string    `toml:"listen"`
	Clusters []Cluster `toml:"clusters"`
}

func Load(args []string) (Config, error) {
	c := Config{Listen: "127.0.0.1:18084", Clusters: []Cluster{}}
	fs := flag.NewFlagSet("mysql-explorer", flag.ContinueOnError)
	var file, listen string
	fs.StringVar(&file, "config", DefaultPath(), "TOML configuration file")
	fs.StringVar(&listen, "listen", "", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() != 0 {
		return c, fmt.Errorf("unexpected positional arguments")
	}
	loaded, err := Read(file)
	if err != nil {
		return c, err
	}
	c = loaded
	if listen != "" {
		c.Listen = listen
	}
	if _, err := net.ResolveTCPAddr("tcp", c.Listen); err != nil {
		return c, fmt.Errorf("invalid listen address")
	}
	return c, nil
}

// Read loads shared configuration without interpreting command-line arguments.
func Read(file string) (Config, error) {
	c := Config{Listen: "127.0.0.1:18084", Clusters: []Cluster{}}
	raw, err := os.ReadFile(file)
	if err == nil {
		if err := toml.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("配置文件 TOML 格式无效")
		}
	} else if !os.IsNotExist(err) {
		return c, fmt.Errorf("无法读取配置文件")
	}
	for i := range c.Clusters {
		cl := &c.Clusters[i]
		if cl.Type == "" {
			cl.Type = "mysql"
		}
		if cl.Type != "mysql" && cl.Type != "tidb" {
			return c, fmt.Errorf("连接类型必须是 mysql 或 tidb")
		}
		if cl.PasswordEnv != "" {
			cl.Password = os.Getenv(cl.PasswordEnv)
		}
		if strings.TrimSpace(cl.Name) == "" || cl.User == "" {
			return c, fmt.Errorf("service name and user are required")
		}
		host, port, err := net.SplitHostPort(cl.Address)
		if err != nil || host == "" || port == "" {
			return c, fmt.Errorf("invalid address for service %q", cl.Name)
		}
	}
	return c, nil
}

// DefaultPath preserves local configuration precedence and supports installed CLI use.
func DefaultPath() string {
	if _, err := os.Stat("config.toml"); err == nil {
		return "config.toml"
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mysql-explorer", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "mysql-explorer", "config.toml")
	}
	return "config.toml"
}
