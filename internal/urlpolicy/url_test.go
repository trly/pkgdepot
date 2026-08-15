package urlpolicy

import "testing"

func TestValidateAcceptsHTTPS(t *testing.T) {
	for _, value := range []string{
		"https://example.com",
		"https://registry.archlinux.org",
		"https://127.0.0.1:8080",
		"https://localhost:8080",
	} {
		if err := Validate(value, "url"); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", value, err)
		}
	}
}

func TestValidateAcceptsLoopbackHTTP(t *testing.T) {
	for _, value := range []string{
		"http://127.0.0.1:8080",
		"http://127.0.0.1",
		"http://[::1]:9090",
		"http://[::1]",
		"http://localhost:8080",
		"http://localhost",
	} {
		if err := Validate(value, "url"); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", value, err)
		}
	}
}

func TestValidateRejectsNonLoopbackHTTP(t *testing.T) {
	for _, value := range []string{
		"http://192.168.1.1:8080",
		"http://10.0.0.1:8080",
		"http://example.com",
		"http://myhost.local:8080",
		"ftp://localhost",
		"//localhost",
	} {
		if err := Validate(value, "url"); err == nil {
			t.Errorf("Validate(%q) = nil, want error", value)
		}
	}
}

func TestValidateRejectsMalformedURLs(t *testing.T) {
	for _, value := range []string{
		"",
		"://",
		"http://",
		"https://",
		"http://user:pass@example.com",
		"https://example.com?query=1",
		"https://example.com#fragment",
		"https://example.com\rinjection",
		"https://example.com\ninjection",
	} {
		if err := Validate(value, "url"); err == nil {
			t.Errorf("Validate(%q) = nil, want error", value)
		}
	}
}

func TestValidateEndpointAcceptsQueryParameters(t *testing.T) {
	for _, value := range []string{
		"https://example.com/authorize?tenant=acme",
		"https://example.com/token?foo=bar",
		"https://id.example/jwks?kid=abc",
		"https://example.com",
		"http://localhost:8080/token?x=1",
		"http://127.0.0.1:8080/authorize?y=2",
	} {
		if err := ValidateEndpoint(value, "url"); err != nil {
			t.Errorf("ValidateEndpoint(%q) = %v, want nil", value, err)
		}
	}
}

func TestValidateEndpointRejectsMalformedURLs(t *testing.T) {
	for _, value := range []string{
		"",
		"://",
		"http://",
		"https://",
		"http://user:pass@example.com",
		"https://example.com#fragment",
		"https://example.com\rinjection",
		"https://example.com\ninjection",
		"http://example.com",
		"http://192.168.1.1:8080?x=1",
		"http://example.com?x=1",
	} {
		if err := ValidateEndpoint(value, "url"); err == nil {
			t.Errorf("ValidateEndpoint(%q) = nil, want error", value)
		}
	}
}

func TestValidateRejectsQueryButValidateEndpointAccepts(t *testing.T) {
	value := "https://example.com?query=1"
	if err := Validate(value, "url"); err == nil {
		t.Errorf("Validate(%q) = nil, want error", value)
	}
	if err := ValidateEndpoint(value, "url"); err != nil {
		t.Errorf("ValidateEndpoint(%q) = %v, want nil", value, err)
	}
}
