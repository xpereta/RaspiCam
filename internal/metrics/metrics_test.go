package metrics

import "testing"

func TestParseVcgencmdFloat(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		prefix string
		suffix string
		want   float64
	}{
		{"temp", "temp=46.8'C", "temp=", "'C", 46.8},
		{"volts", "volt=1.2000V", "volt=", "V", 1.2},
	}

	for _, tc := range cases {
		got, err := parseVcgencmdFloat(tc.out, tc.prefix, tc.suffix)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseHexValue(t *testing.T) {
	got, err := parseHexValue("throttled=0x50005", "throttled=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0x50005 {
		t.Fatalf("got %x want %x", got, 0x50005)
	}
}
