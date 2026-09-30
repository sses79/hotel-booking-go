package config

import (
	"fmt"
	"os"
	"regexp"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type Config struct {
	Env      string
	Project  string
	Instance string
	Database string
}

func Load() (Config, error) {
	c := Config{
		Env:      value("APP_ENV", "production"),
		Project:  value("SPANNER_PROJECT", "hotel-booking-local"),
		Instance: value("SPANNER_INSTANCE", "hotel-booking"),
		Database: value("SPANNER_DATABASE", "hotel-booking"),
	}
	for label, id := range map[string]string{"project": c.Project, "instance": c.Instance, "database": c.Database} {
		if !idPattern.MatchString(id) {
			return Config{}, fmt.Errorf("invalid Spanner %s ID %q", label, id)
		}
	}
	return c, nil
}

func (c Config) DatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", c.Project, c.Instance, c.Database)
}

func (c Config) InstancePath() string {
	return fmt.Sprintf("projects/%s/instances/%s", c.Project, c.Instance)
}

func (c Config) DemoRoutesEnabled() bool {
	return c.Env == "local" || c.Env == "demo"
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
