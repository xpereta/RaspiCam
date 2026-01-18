package focusassist

import (
	"bytes"
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

func (c *CommandCapturer) Capture(ctx context.Context, lensPosition float64, settings CaptureSettings) (CaptureResult, error) {
	if c == nil {
		return CaptureResult{}, errors.New("capturer is nil")
	}
	if c.command == "" {
		return CaptureResult{}, errors.New("capture command not set")
	}

	path, err := os.CreateTemp("", "focus-assist-*.jpg")
	if err != nil {
		return CaptureResult{}, fmt.Errorf("create temp file: %w", err)
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
	args = append(args, captureSettingsArgs(settings)...)

	cmd := exec.CommandContext(ctx, c.command, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return CaptureResult{Output: output.String()}, fmt.Errorf("capture command failed after %s: %w", time.Since(start), err)
	}

	data, err := os.ReadFile(name)
	if err != nil {
		return CaptureResult{Output: output.String()}, fmt.Errorf("read capture output: %w", err)
	}
	if len(data) == 0 {
		return CaptureResult{Output: output.String()}, errors.New("empty capture output")
	}
	return CaptureResult{Data: data, Output: output.String()}, nil
}

func captureSettingsArgs(settings CaptureSettings) []string {
	args := []string{}
	if settings.VFlip {
		args = append(args, "--vflip")
	}
	if settings.HFlip {
		args = append(args, "--hflip")
	}
	if settings.Width > 0 && settings.Height > 0 {
		args = append(args, "--width", strconv.Itoa(settings.Width))
		args = append(args, "--height", strconv.Itoa(settings.Height))
	}
	if settings.Mode != "" {
		args = append(args, "--mode", settings.Mode)
	}
	return args
}
