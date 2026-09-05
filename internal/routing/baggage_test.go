package routing

import (
	"regexp"
	"testing"
)

func TestBaggageMemberBoundaries(t *testing.T) {
	match := regexp.MustCompile(BaggagePattern("cmp_123"))
	for _, tt := range []struct {
		name, header string
		want         bool
	}{
		{"canonical", "composition=cmp_123", true},
		{"other members", "tenant=demo,composition=cmp_123,locale=en", true},
		{"whitespace", "tenant=demo, \tcomposition = cmp_123 \t,locale=en", true},
		{"properties", "composition=cmp_123;sampled;other=value", true},
		{"properties with other member", "composition=cmp_123;sampled,locale=en", true},
		{"longer id", "composition=cmp_1234", false},
		{"longer key", "othercomposition=cmp_123", false},
		{"key in value", "other=composition=cmp_123", false},
		{"different key case", "Composition=cmp_123", false},
		{"missing", "locale=en", false},
		{"empty", "", false},
		{"encoded id outside contract", "composition=cmp%5F123", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := match.MatchString(tt.header); got != tt.want {
				t.Fatalf("match %q = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestBaggagePatternEscapesID(t *testing.T) {
	match := regexp.MustCompile(BaggagePattern("cmp_123.any"))
	if !match.MatchString("composition=cmp_123.any") || match.MatchString("composition=cmp_123Xany") {
		t.Fatal("ID must be escaped as a literal")
	}
}
