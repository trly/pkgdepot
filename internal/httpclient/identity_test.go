package httpclient

import (
	"strings"
	"testing"
)

func TestSafePictureURL(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "https", value: "https://id.example/profile.png", want: "https://id.example/profile.png"},
		{name: "http development", value: "http://127.0.0.1/profile.png", want: "http://127.0.0.1/profile.png"},
		{name: "javascript", value: "javascript:alert(1)", want: ""},
		{name: "userinfo", value: "https://user:pass@id.example/profile.png", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := safePictureURL(test.value); got != test.want {
				t.Fatalf("safePictureURL(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestScopePageEscapesIdentityAndDisplaysPhoto(t *testing.T) {
	page := scopePage(authenticatedUser{
		Name:    `<script>alert("x")</script>`,
		Picture: "https://id.example/avatar.png",
	}, []string{"package:publish"}, map[string]bool{"package:publish": true})
	if strings.Contains(page, "<script>alert") || !strings.Contains(page, "avatar.png") || !strings.Contains(page, "Publish packages") {
		t.Fatalf("scope page contains unsafe or missing content: %s", page)
	}
}
