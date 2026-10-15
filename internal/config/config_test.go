package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrecedencePartialLimitsAndRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "profile"))
	t.Setenv("FANOUTFUSE_HOME", filepath.Join(home, "state"))
	p := filepath.Join(home, ".config", "fanoutfuse")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "config.toml"), []byte("[limits]\nspawn_rate_per_min = {trip = 22}\n[general]\nescalate_after = '4s'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(project, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".fanoutfuse.toml"), []byte("[limits]\nspawn_rate_per_min = {warn = 12}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filepath.Join(project, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits["spawn_rate"].Trip != 22 || c.Limits["spawn_rate"].Warn != 12 {
		t.Fatal("partial merge failed", c.Limits)
	}
	if c.Home != filepath.Join(home, "state") || c.ClaudeRoot != filepath.Join(home, "profile") {
		t.Fatal("env roots ignored")
	}
	t.Setenv("FANOUTFUSE_SPAWN_RATE_TRIP", "3")
	c, err = Load(project)
	if err != nil || c.Limits["spawn_rate"].Trip != 3 {
		t.Fatal("env limit ignored", err)
	}
	t.Setenv("FANOUTFUSE_SPAWN_RATE_TRIP", "NaN")
	if _, err = Load(project); err == nil {
		t.Fatal("accepted NaN")
	}
}

func TestEveryRuleEnvironmentOverride(t *testing.T) {
	for _, name := range Names {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("FANOUTFUSE_"+strings.ToUpper(name)+"_TRIP", "1")
			t.Setenv("FANOUTFUSE_"+strings.ToUpper(name)+"_WARN", "0.5")
			c, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if c.Limits[name].Trip != 1 || c.Limits[name].Warn != 0.5 {
				t.Fatal(c.Limits[name])
			}
		})
	}
}
