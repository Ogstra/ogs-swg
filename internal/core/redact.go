package core

import "regexp"

var (
	// srcRe matches "from " followed by an IPv4 address or IPv6 bracket address with port.
	srcRe = regexp.MustCompile(`from (\[?[\w:.]+\]?:\d+)`)
	// dstRe matches "to " followed by a host/IP with port.
	dstRe = regexp.MustCompile(`to ([\w:.[\]-]+:\d+)`)
)

// CensorLine returns the log line with source IP and destination host:port
// replaced by "***". Lines that do not match the sing-box accepted/rejected
// connection-log format are returned unchanged.
func CensorLine(line string) string {
	replaced := srcRe.ReplaceAllString(line, "from ***:***")
	replaced = dstRe.ReplaceAllString(replaced, "to ***:***")
	return replaced
}
