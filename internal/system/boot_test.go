package system

import (
	"testing"
	"time"
)

func TestParseUptime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "valid", in: "12345.67 54321.00\n", want: 12345*time.Second + 670*time.Millisecond},
		{name: "integer", in: "42 1", want: 42 * time.Second},
		{name: "leading whitespace", in: "  3.5 7.0", want: 3500 * time.Millisecond},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: " \n", wantErr: true},
		{name: "garbage", in: "abc def", wantErr: true},
		{name: "negative", in: "-1.0 2.0", wantErr: true},
		{name: "nan", in: "NaN 2.0", wantErr: true},
		{name: "overflow", in: "1e300 2.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUptime(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
