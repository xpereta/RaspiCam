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
