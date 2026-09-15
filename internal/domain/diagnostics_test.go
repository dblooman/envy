package domain

import "testing"

func TestDiagnosticBounds(t *testing.T) {
	for _, o := range []LogOptions{{TailLines: -1}, {TailLines: 1001}, {MaxBytes: -1}, {MaxBytes: 262145}, {SinceSeconds: -1}, {SinceSeconds: 86401}} {
		if _, err := NormalizeLogOptions(o); err == nil {
			t.Fatalf("unbounded options accepted: %+v", o)
		}
	}

	defaults, err := NormalizeLogOptions(LogOptions{})
	if err != nil || defaults.TailLines != 200 || defaults.MaxBytes != 65536 {
		t.Fatalf("defaults %+v: %v", defaults, err)
	}

	for _, cursor := range []string{"-1", "0", "01", "+1", "1.2", "abc", "9223372036854775808"} {
		if _, err := EventCursor(cursor); err == nil {
			t.Fatalf("invalid cursor %q", cursor)
		}
	}

	for _, cursor := range []string{"", "1", "9223372036854775807"} {
		if _, err := EventCursor(cursor); err != nil {
			t.Fatal(err)
		}
	}
}
