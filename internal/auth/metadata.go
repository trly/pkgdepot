package auth

import "net/url"

type ResourceServer struct {
	Validator Validator
	Metadata  ResourceMetadata
	Authorize func(Claims, string, string, string) bool
}

type ResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// NormalizeResourceIdentifier parses a URL string and re-serializes it,
// normalizing percent-encoding and trailing slashes so that two
// semantically equivalent URLs compare equal.
func NormalizeResourceIdentifier(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	parsed.Path = trimTrailingSlash(parsed.Path)
	if parsed.RawPath != "" {
		parsed.RawPath = trimTrailingSlash(parsed.RawPath)
	}
	return parsed.String()
}

func trimTrailingSlash(s string) string {
	for len(s) > 1 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
