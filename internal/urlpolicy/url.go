package urlpolicy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validate requires HTTPS, except for loopback addresses (localhost or
// loopback IP literals) used for local development.
func Validate(value, name string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user info, query, or fragment", name)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("%s must use HTTPS (HTTP is allowed only for localhost or loopback IP literals)", name)
	}
	return nil
}

func ValidateEndpoint(value, name string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user info or fragment", name)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("%s must use HTTPS (HTTP is allowed only for localhost or loopback IP literals)", name)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
