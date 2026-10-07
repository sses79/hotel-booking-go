package config

import (
	"strings"
	"testing"
)

func TestOracleConfiguration(t *testing.T) {
	t.Setenv("ORACLE_HOST", "")
	t.Setenv("ORACLE_PORT", "")
	t.Setenv("ORACLE_SERVICE", "")
	t.Setenv("ORACLE_USER", "hotel_booking")
	t.Setenv("ORACLE_PASSWORD", "sensitive@:/?#&password")
	t.Setenv("APP_ENV", "")
	cfg, err := Load()
	if err != nil || cfg.OracleHost != "localhost" || cfg.OraclePort != 1521 || cfg.OracleService != "FREEPDB1" || cfg.DemoRoutesEnabled() {
		t.Fatalf("unexpected defaults or environment: %v", err)
	}
	for _, port := range []string{"0", "65536", "bad", "-1"} {
		t.Setenv("ORACLE_PORT", port)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted invalid port %q", port)
		}
	}
	t.Setenv("ORACLE_PORT", "1521")
	t.Setenv("ORACLE_USER", "")
	if _, err := Load(); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("missing user must fail without leaking credentials")
	}
	t.Setenv("ORACLE_USER", "hotel_booking")
	t.Setenv("ORACLE_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing password must fail")
	}
}
