package system

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseCanPowerOff(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		want       bool
		wantReason string
		wantErr    bool
	}{
		{name: "yes", in: `s "yes"`, want: true},
		{name: "yes with newline", in: "s \"yes\"\n", want: true},
		{name: "yes extra whitespace", in: "  s   \"yes\"  \n", want: true},
		{name: "no", in: `s "no"`, wantReason: "not permitted (polkit answer: no); see SYSTEM.md polkit rule"},
		{name: "challenge", in: "s \"challenge\"\n", wantReason: "not permitted (polkit answer: challenge); see SYSTEM.md polkit rule"},
		{name: "na", in: `s "na"`, wantReason: "power off not available on this system (logind answer: na)"},
		{name: "unknown answer", in: `s "maybe"`, wantReason: `unexpected logind answer "maybe"; see SYSTEM.md polkit rule`},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: " \n", wantErr: true},
		{name: "garbage", in: "Call failed: Access denied", wantErr: true},
		{name: "wrong type", in: `b true`, wantErr: true},
		{name: "unquoted", in: `s yes`, wantErr: true},
		{name: "missing value", in: `s`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason, err := parseCanPowerOff(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got ok=%v reason=%q", got, reason)
				}
				if got {
					t.Fatalf("expected false on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

type fakeRunner struct {
	name string
	args []string
	out  []byte
	err  error
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.name = name
	f.args = args
	return f.out, f.err
}

func TestSystemdPowerPowerOff(t *testing.T) {
	errExit := errors.New("exit status 1")
	tests := []struct {
		name    string
		out     string
		err     error
		wantErr string
	}{
		{name: "success"},
		{name: "error with output", out: "Access denied\n", err: errExit, wantErr: "systemctl poweroff: exit status 1: Access denied"},
		{name: "error without output", out: " \n", err: errExit, wantErr: "systemctl poweroff: exit status 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRunner{out: []byte(tt.out), err: tt.err}
			p := SystemdPower{run: f.run}
			err := p.PowerOff(context.Background())
			if f.name != "systemctl" || !reflect.DeepEqual(f.args, []string{"poweroff"}) {
				t.Fatalf("unexpected command: %s %v", f.name, f.args)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error %v, want %q", err, tt.wantErr)
			}
			if !errors.Is(err, errExit) {
				t.Fatalf("error not wrapped: %v", err)
			}
		})
	}
}

func TestSystemdPowerCanPowerOff(t *testing.T) {
	wantArgs := []string{
		"call",
		"org.freedesktop.login1",
		"/org/freedesktop/login1",
		"org.freedesktop.login1.Manager",
		"CanPowerOff",
	}
	tests := []struct {
		name         string
		out          string
		err          error
		want         bool
		reasonPrefix string
	}{
		{name: "yes", out: "s \"yes\"\n", want: true},
		{name: "challenge", out: "s \"challenge\"\n", reasonPrefix: "not permitted (polkit answer: challenge)"},
		{name: "garbage", out: "???", reasonPrefix: "permission check failed: unexpected busctl output"},
		{name: "exec error with output", out: "Failed to connect to bus\n", err: errors.New("exit status 1"), reasonPrefix: "permission check failed: exit status 1: Failed to connect to bus"},
		{name: "exec error without output", err: errors.New("executable file not found"), reasonPrefix: "permission check failed: executable file not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeRunner{out: []byte(tt.out), err: tt.err}
			p := SystemdPower{run: f.run}
			got, reason := p.CanPowerOff(context.Background())
			if f.name != "busctl" || !reflect.DeepEqual(f.args, wantArgs) {
				t.Fatalf("unexpected command: %s %v", f.name, f.args)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v (reason %q)", got, tt.want, reason)
			}
			if tt.want {
				if reason != "" {
					t.Fatalf("unexpected reason: %q", reason)
				}
				return
			}
			if !strings.HasPrefix(reason, tt.reasonPrefix) {
				t.Fatalf("reason %q, want prefix %q", reason, tt.reasonPrefix)
			}
		})
	}
}
