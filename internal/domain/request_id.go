package domain

import "regexp"

var requestIDPattern = regexp.MustCompile(`^([A-Fa-f0-9]{16,64}|[A-Fa-f0-9]{8}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{4}-[A-Fa-f0-9]{12})$`)

// ValidRequestID admits only bounded hexadecimal and UUID-like identifiers.
// Arbitrary response headers are never copied into evidence or telemetry links.
func ValidRequestID(value string) bool { return requestIDPattern.MatchString(value) }
