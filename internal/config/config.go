package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Env            string
	OracleHost     string
	OraclePort     int
	OracleService  string
	OracleUser     string
	OraclePassword string
}

func Load() (Config, error) {
	port, err := strconv.Atoi(value("ORACLE_PORT", "1521"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("ORACLE_PORT must be an integer between 1 and 65535")
	}
	c := Config{
		Env:            value("APP_ENV", "production"),
		OracleHost:     value("ORACLE_HOST", "localhost"),
		OraclePort:     port,
		OracleService:  value("ORACLE_SERVICE", "FREEPDB1"),
		OracleUser:     os.Getenv("ORACLE_USER"),
		OraclePassword: os.Getenv("ORACLE_PASSWORD"),
	}
	for _, field := range []struct{ name, value string }{
		{"ORACLE_HOST", c.OracleHost}, {"ORACLE_SERVICE", c.OracleService},
		{"ORACLE_USER", c.OracleUser}, {"ORACLE_PASSWORD", c.OraclePassword},
	} {
		if strings.TrimSpace(field.value) == "" {
			return Config{}, fmt.Errorf("%s is required", field.name)
		}
	}
	return c, nil
}

func (c Config) DemoRoutesEnabled() bool { return c.Env == "local" || c.Env == "demo" }

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
