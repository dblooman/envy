// Package routing compiles provider-independent composition routing decisions.
package routing

import "regexp"

// BaggagePattern matches a canonical composition member in a serialized baggage
// header. It deliberately does not implement the complete W3C baggage grammar:
// ingress must establish a single unambiguous, platform-issued composition key.
// Envoy regex header matching evaluates the whole header, hence both anchors.
func BaggagePattern(id string) string {
	return `^(.*,)?[ \t]*composition[ \t]*=[ \t]*` + regexp.QuoteMeta(id) + `[ \t]*(;[^,]*)?(,.*)?$`
}
