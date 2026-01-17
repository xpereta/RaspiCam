package focusassist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CommandCapturer struct {
	command string
	args    []string
}

func NewCommandCapturer() (Capturer, error) {
	if path, err := exec.LookPath("rpicam-still"); err == nil {
		return &CommandCapturer{
			command: path,
			args:    []string{"--nopreview", "--timeout", "1", "--lens-position", "{lens}", "--output", "{output}"},
		}, nil
	}
	if path, err := exec.LookPath("libcamera-still"); err == nil {
		return &CommandCapturer{
			command: path,
			args:    []string{"--nopreview", "--timeout", "1", "--lens-position", "{lens}", "-o", "{output}"},
		}, nil
	}
	return nil, errors.New("no capture command found")
}

func (c *CommandCapturer) Capture(ctx context.Context, lensPosition float64) ([]byte, error) {
	if c == nil {
		return nil, errors.New("capturer is nil")
	}
	if c.command == "" {
		return nil, errors.New("capture command not set")
	}

	path, err := os.CreateTemp("", "focus-assist-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	name := path.Name()
	_ = path.Close()
	defer os.Remove(name)

	args := make([]string, 0, len(c.args))
	lens := strconv.FormatFloat(lensPosition, 'f', 2, 64)
	for _, arg := range c.args {
		replaced := strings.ReplaceAll(arg, "{lens}", lens)
		replaced = strings.ReplaceAll(replaced, "{output}", name)
		args = append(args, replaced)
	}

	cmd := exec.CommandContext(ctx, c.command, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("capture command failed after %s: %w", time.Since(start), err)
	}

	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read capture output: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("empty capture output")
	}
	return data, nil
}
