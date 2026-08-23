package httpclient

import (
	"context"
	"net/http"
	"net/url"
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
	}, []string{"package:publish"}, map[string]bool{"package:publish": true}, "csrf-token")
	if strings.Contains(page, "<script>alert") || !strings.Contains(page, "avatar.png") || !strings.Contains(page, "Publish packages") {
		t.Fatalf("scope page contains unsafe or missing content: %s", page)
	}
}

func TestSelectScopesRejectsMissingCSRFToken(t *testing.T) {
	client := New(context.Background(), "http://packages.example")
	client.resourceScopes = []string{"package:publish"}
	client.OAuth.AuthorizationPrompt = func(pageURL string) {
		parsed, err := url.Parse(pageURL)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.Get(pageURL)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("scope page status = %d", response.StatusCode)
		}

		response, err = http.PostForm(pageURL, url.Values{"scope": {"package:publish"}})
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("missing token status = %d", response.StatusCode)
		}

		response, err = http.PostForm(pageURL, url.Values{
			"csrf_token": {parsed.Query().Get("token")},
			"scope":      {"package:publish"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("valid token status = %d", response.StatusCode)
		}
	}

	selected, err := client.selectScopes(context.Background(), authenticatedUser{Name: "Test User"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0] != "package:publish" {
		t.Fatalf("selected scopes = %v", selected)
	}
}
