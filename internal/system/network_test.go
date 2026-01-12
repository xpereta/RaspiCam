package system

import "testing"

func TestParseNetDevLine(t *testing.T) {
	line := "wlan0: 12345 0 0 0 0 0 0 0 67890 0 0 0 0 0 0 0"
	rx, tx, ok, err := parseNetDevLine(line, "wlan0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected match")
	}
	if rx != 12345 || tx != 67890 {
		t.Fatalf("unexpected values: rx=%d tx=%d", rx, tx)
	}

	_, _, ok, err = parseNetDevLine(line, "eth0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected no match")
	}
}

func TestParseWirelessLine(t *testing.T) {
	line := "wlan0: 0000   54.  -42.  0.  0 0 0 0 0 0"
	quality, ok, err := parseWirelessLine(line, "wlan0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected match")
	}
	if quality != "54/70 (-42 dBm)" {
		t.Fatalf("unexpected quality: %q", quality)
	}

	_, ok, err = parseWirelessLine(line, "eth0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected no match")
	}
}
