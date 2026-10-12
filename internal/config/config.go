package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Cluster keeps credentials private; PasswordEnv overrides the configured password.
type Cluster struct {
	Name        string `toml:"name"`
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
	fs.StringVar(&file, "config", "config.toml", "TOML configuration file")
	fs.StringVar(&listen, "listen", "", "HTTP bind address")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() != 0 {
		return c, fmt.Errorf("unexpected positional arguments")
	}
	raw, err := os.ReadFile(file)
	if err == nil {
		if err := toml.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("invalid TOML configuration")
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	if listen != "" {
		c.Listen = listen
	}
	if _, err := net.ResolveTCPAddr("tcp", c.Listen); err != nil {
		return c, fmt.Errorf("invalid listen address")
	}
	for i := range c.Clusters {
		cl := &c.Clusters[i]
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
