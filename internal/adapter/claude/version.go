package claude

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func Version(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "claude", "--version").Output()
	if err != nil {
		return "unknown"
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 128 {
		return "unknown"
	}
	return s
}
