package system

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type SystemctlService struct {
	name string
}

func NewSystemctlService(name string) *SystemctlService {
	return &SystemctlService{name: name}
}

func (s *SystemctlService) Start(ctx context.Context) error {
	return s.run(ctx, "start")
}

func (s *SystemctlService) Stop(ctx context.Context) error {
	return s.run(ctx, "stop")
}

func (s *SystemctlService) CanControl(ctx context.Context) (bool, string) {
	cmd := exec.CommandContext(ctx, "systemctl", "start", "--dry-run", s.name)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, ""
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return false, err.Error()
	}
	return false, controlErrorMessage(output)
}

func (s *SystemctlService) run(ctx context.Context, action string) error {
	cmd := exec.CommandContext(ctx, "systemctl", action, s.name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if output == "" {
			return err
		}
		return fmt.Errorf("systemctl %s %s: %s", action, s.name, output)
	}
	return nil
}

func controlErrorMessage(output string) string {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "authentication is required") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "permission denied") {
		return "MediaMTX control requires systemd permissions."
	}
	return output
}
