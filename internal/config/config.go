// Package config resolves defaults, user and project TOML, and environment overrides.
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Limit struct {
	Warn    float64 `toml:"warn"`
	Trip    float64 `toml:"trip"`
	MinWarn int     `toml:"min_n_warn"`
	MinTrip int     `toml:"min_n_trip"`
}
type General struct {
	Ladder   []string `toml:"action_ladder"`
	Escalate string   `toml:"escalate_after"`
	Notify   bool     `toml:"notify"`
	Bell     bool     `toml:"bell"`
}
type Config struct {
	General  General          `toml:"general"`
	Limits   map[string]Limit `toml:"limits"`
	Cooldown struct {
		AfterResume string `toml:"after_resume"`
	} `toml:"cooldown"`
	Adapters struct {
		Claude struct {
			Root    string `toml:"root"`
			Enabled bool   `toml:"enabled"`
		} `toml:"claude"`
	} `toml:"adapters"`
	Home, ClaudeRoot string            `toml:"-"`
	Sources          map[string]string `toml:"-"`
}

var Names = []string{"spawn_rate", "spawn_burst", "total_spawns", "max_depth", "running_concurrent", "tokens_per_min", "growth_factor", "breadth_per_parent"}
var aliases = map[string]string{"spawn_rate_per_min": "spawn_rate", "spawn_burst_per_10s": "spawn_burst", "growth_factor_30s": "growth_factor"}

func Default() Config {
	c := Config{General: General{Ladder: []string{"warn", "contain", "stop_background"}, Escalate: "20s", Notify: true, Bell: true}, Limits: map[string]Limit{}, Sources: map[string]string{}}
	vals := [][2]float64{{15, 30}, {8, 15}, {40, 80}, {3, 4}, {15, 25}, {0, 0}, {3, 4}, {6, 10}}
	for i, n := range Names {
		c.Limits[n] = Limit{Warn: vals[i][0], Trip: vals[i][1]}
		c.Sources[n] = "default"
	}
	l := c.Limits["growth_factor"]
	l.MinWarn = 10
	l.MinTrip = 20
	c.Limits["growth_factor"] = l
	for _, key := range []string{"general.action_ladder", "general.escalate_after", "general.notify", "general.bell", "cooldown.after_resume", "claude_root", "home"} {
		c.Sources[key] = "default"
	}
	c.Cooldown.AfterResume = "10m"
	return c
}
func Home() string {
	if v := os.Getenv("FANOUTFUSE_HOME"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".fanoutfuse")
}
func ClaudeRoot() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".claude")
}
func Load(cwd string) (Config, error) {
	c := Default()
	c.Home = Home()
	c.ClaudeRoot = ClaudeRoot()
	if os.Getenv("FANOUTFUSE_HOME") != "" {
		c.Sources["home"] = "FANOUTFUSE_HOME"
	}
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		c.Sources["claude_root"] = "CLAUDE_CONFIG_DIR"
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	paths := []string{filepath.Join(base, "fanoutfuse", "config.toml")}
	for p := cwd; p != ""; p = filepath.Dir(p) {
		f := filepath.Join(p, ".fanoutfuse.toml")
		if _, err := os.Stat(f); err == nil {
			paths = append(paths, f)
			break
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return c, err
		}
		var raw struct {
			Limits map[string]Limit `toml:"limits"`
		}
		if err = toml.Unmarshal(b, &raw); err != nil {
			return c, fmt.Errorf("%s: %w", p, err)
		}
		// Merge each limit field rather than replacing a partially specified limit.
		var doc map[string]any
		if err = toml.Unmarshal(b, &doc); err != nil {
			return c, err
		}
		if ls, ok := doc["limits"].(map[string]any); ok {
			for a, n := range aliases {
				if v, yes := ls[a]; yes {
					ls[n] = v
					delete(ls, a)
				}
			}
			for name, value := range ls {
				limit, known := c.Limits[name]
				if !known {
					return c, fmt.Errorf("unknown limit %q in %s", name, p)
				}
				fields, ok := value.(map[string]any)
				if !ok {
					return c, fmt.Errorf("limit %s must be a table", name)
				}
				merged := map[string]any{"warn": limit.Warn, "trip": limit.Trip, "min_n_warn": limit.MinWarn, "min_n_trip": limit.MinTrip}
				for key, val := range fields {
					merged[key] = val
				}
				ls[name] = merged
			}
		}
		for _, section := range []string{"general", "cooldown"} {
			if fields, ok := doc[section].(map[string]any); ok {
				for key := range fields {
					c.Sources[section+"."+key] = p
				}
			}
		}
		if fields, ok := doc["adapters"].(map[string]any); ok {
			if cl, ok := fields["claude"].(map[string]any); ok && cl["root"] != nil && os.Getenv("CLAUDE_CONFIG_DIR") == "" {
				c.Sources["claude_root"] = p
			}
		}
		normalized, err := toml.Marshal(doc)
		if err != nil {
			return c, err
		}
		if err = toml.Unmarshal(normalized, &c); err != nil {
			return c, err
		}
		for n := range raw.Limits {
			if a, ok := aliases[n]; ok {
				n = a
			}
			c.Sources[n] = p
		}
	}
	if c.Adapters.Claude.Root != "" && os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		c.ClaudeRoot = expand(c.Adapters.Claude.Root)
	}
	for _, n := range Names {
		l := c.Limits[n]
		for _, part := range []string{"WARN", "TRIP", "MIN_N_WARN", "MIN_N_TRIP"} {
			key := "FANOUTFUSE_" + strings.ToUpper(n) + "_" + part
			v := os.Getenv(key)
			if v == "" {
				for a, target := range aliases {
					if target == n {
						v = os.Getenv("FANOUTFUSE_" + strings.ToUpper(a) + "_" + part)
					}
				}
			}
			if v == "" {
				continue
			}
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				return c, fmt.Errorf("invalid %s", key)
			}
			switch part {
			case "WARN":
				l.Warn = f
			case "TRIP":
				l.Trip = f
			case "MIN_N_WARN":
				l.MinWarn = int(f)
			case "MIN_N_TRIP":
				l.MinTrip = int(f)
			}
			c.Sources[n] = key
		}
		c.Limits[n] = l
	}
	if v := os.Getenv("FANOUTFUSE_ESCALATE_AFTER"); v != "" {
		c.General.Escalate = v
		c.Sources["general.escalate_after"] = "FANOUTFUSE_ESCALATE_AFTER"
	}
	if v := os.Getenv("FANOUTFUSE_ACTION_LADDER"); v != "" {
		c.General.Ladder = strings.Split(v, ",")
		c.Sources["general.action_ladder"] = "FANOUTFUSE_ACTION_LADDER"
	}
	if d, err := time.ParseDuration(c.General.Escalate); err != nil || d <= 0 {
		return c, fmt.Errorf("escalate_after must be a positive duration")
	}
	if _, err := time.ParseDuration(c.Cooldown.AfterResume); err != nil {
		return c, err
	}
	for _, s := range c.General.Ladder {
		switch s {
		case "warn", "contain", "stop_background", "pause", "kill":
		default:
			return c, fmt.Errorf("unknown ladder step %q", s)
		}
	}
	return c, nil
}
func expand(s string) string {
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(os.Getenv("HOME"), s[2:])
	}
	return s
}
func (c Config) EscalateAfter() time.Duration {
	d, _ := time.ParseDuration(c.General.Escalate)
	return d
}
func (c Config) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "home = %s (%s)\nclaude_root = %s (%s)\nstore_prompts = false (hard-wired)\n", c.Home, c.Sources["home"], c.ClaudeRoot, c.Sources["claude_root"])
	fmt.Fprintf(&b, "action_ladder = %v (%s)\nescalate_after = %s (%s)\nnotify = %t (%s)\nbell = %t (%s)\nafter_resume = %s (%s)\n", c.General.Ladder, c.Sources["general.action_ladder"], c.General.Escalate, c.Sources["general.escalate_after"], c.General.Notify, c.Sources["general.notify"], c.General.Bell, c.Sources["general.bell"], c.Cooldown.AfterResume, c.Sources["cooldown.after_resume"])
	for _, n := range Names {
		l := c.Limits[n]
		fmt.Fprintf(&b, "%s: warn=%g trip=%g min_n_warn=%d min_n_trip=%d (%s)\n", n, l.Warn, l.Trip, l.MinWarn, l.MinTrip, c.Sources[n])
	}
	return b.String()
}
