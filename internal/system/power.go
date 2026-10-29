package system

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// polkitHint points the operator at the documented polkit rule.
const polkitHint = "see SYSTEM.md polkit rule"

// runFunc executes a command and returns its combined output.
type runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// SystemdPower powers off the system through systemd and checks whether the
// current user is allowed to do so via logind. The zero value is ready to use
// and runs real commands.
type SystemdPower struct {
	run runFunc
}

func (p SystemdPower) runner() runFunc {
	if p.run != nil {
		return p.run
	}
	return execCombined
}

func execCombined(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// PowerOff runs "systemctl poweroff". It returns once systemd has accepted
// the job, not when the system is down.
func (p SystemdPower) PowerOff(ctx context.Context) error {
	out, err := p.runner()(ctx, "systemctl", "poweroff")
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("systemctl poweroff: %w: %s", err, msg)
		}
		return fmt.Errorf("systemctl poweroff: %w", err)
	}
	return nil
}

// CanPowerOff asks logind whether the current user may power off the system.
// It returns true only for the answer "yes"; otherwise it returns false and a
// human readable reason for the UI.
func (p SystemdPower) CanPowerOff(ctx context.Context) (bool, string) {
	out, err := p.runner()(ctx, "busctl", "call",
		"org.freedesktop.login1",
		"/org/freedesktop/login1",
		"org.freedesktop.login1.Manager",
		"CanPowerOff")
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return false, fmt.Sprintf("permission check failed: %v: %s", err, msg)
		}
		return false, fmt.Sprintf("permission check failed: %v", err)
	}
	ok, reason, err := parseCanPowerOff(string(out))
	if err != nil {
		return false, fmt.Sprintf("permission check failed: %v", err)
	}
	return ok, reason
}

// CanPowerOff checks power-off permission using the default SystemdPower.
func CanPowerOff(ctx context.Context) (bool, string) {
	return SystemdPower{}.CanPowerOff(ctx)
}

// parseCanPowerOff parses busctl output such as `s "yes"`. Only "yes" means
// permitted. A well-formed but negative answer returns false with a reason and
// a nil error; malformed output returns an error.
func parseCanPowerOff(out string) (bool, string, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return false, "", errors.New("empty busctl output")
	}
	typ, value, found := strings.Cut(s, " ")
	if !found || typ != "s" {
		return false, "", fmt.Errorf("unexpected busctl output: %q", s)
	}
	answer, err := strconv.Unquote(strings.TrimSpace(value))
	if err != nil {
		return false, "", fmt.Errorf("unexpected busctl output: %q", s)
	}
	switch answer {
	case "yes":
		return true, "", nil
	case "no", "challenge":
		return false, fmt.Sprintf("not permitted (polkit answer: %s); %s", answer, polkitHint), nil
	case "na":
		return false, "power off not available on this system (logind answer: na)", nil
	default:
		return false, fmt.Sprintf("unexpected logind answer %q; %s", answer, polkitHint), nil
	}
}
