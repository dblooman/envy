package domain

import "testing"

func TestRequestIDIsBoundedAndCannotCarryURLSyntax(t *testing.T) {
	for _, value := range []string{"123e4567-e89b-12d3-a456-426614174000", "abcdef0123456789"} {
		if !ValidRequestID(value) {
			t.Fatalf("valid observed request ID rejected: %s", value)
		}
	}

	for _, value := range []string{"", "../../secret", "token=abc", "abc?secret=def", "abcdef0123456789%0a", "abcdef0123456789/other"} {
		if ValidRequestID(value) {
			t.Fatalf("unsafe response header accepted: %s", value)
		}
	}
}
