package httpapi_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/trly/pkgdepot/internal/auth"
	"github.com/trly/pkgdepot/internal/httpapi"
	"github.com/trly/pkgdepot/internal/repository"
)

type resourceValidator struct {
	claims auth.Claims
	err    error
}

func (v resourceValidator) Validate(context.Context, string) (auth.Claims, error) {
	if v.err != nil {
		return auth.Claims{}, v.err
	}
	if v.claims.Scopes == nil && v.claims.Roles == nil {
		return auth.Claims{Scopes: []string{"package:publish"}, Roles: []string{"publisher"}}, nil
	}
	return v.claims, nil
}

type commands struct{}

func (commands) Add(context.Context, string, string) error    { return nil }
func (commands) Remove(context.Context, string, string) error { return nil }

func TestHealthAndAuthentication(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{
		ResourceAuth: &auth.ResourceServer{
			Validator: resourceValidator{},
			Authorize: func(claims auth.Claims, scope, _, _ string) bool {
				return auth.AuthorizeRoles(claims, scope, map[string][]string{"publisher": {auth.ScopePublish}}, "")
			},
			Metadata: auth.ResourceMetadata{
				Resource:               "http://localhost:8080",
				BearerMethodsSupported: []string{"header"},
			},
		},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/repositories/test/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	if !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer realm=") {
		t.Fatalf("missing Bearer challenge, got %q", response.Header.Get("WWW-Authenticate"))
	}

	request.Header.Set("Authorization", "Bearer valid")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("authenticated status = %d", response.StatusCode)
	}
}

func TestResourceServerAuthenticationAndMetadata(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{ResourceAuth: &auth.ResourceServer{
		Validator: resourceValidator{},
		Authorize: func(claims auth.Claims, scope, _, _ string) bool {
			return auth.AuthorizeRoles(claims, scope, map[string][]string{"publisher": {auth.ScopePublish}}, "")
		},
		Metadata: auth.ResourceMetadata{
			Resource:               "http://localhost:8080",
			AuthorizationServers:   []string{"https://issuer.example"},
			ScopesSupported:        []string{"package:publish"},
			BearerMethodsSupported: []string{"header"},
		},
	}}))
	defer server.Close()

	response, err := http.Get(server.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metadata status = %d", response.StatusCode)
	}
	var metadata auth.ResourceMetadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Resource != "http://localhost:8080" {
		t.Fatalf("metadata resource = %q", metadata.Resource)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/repositories/stable/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer realm=\"") || strings.Contains(response.Header.Get("WWW-Authenticate"), "error=") {
		t.Fatalf("missing token = %d, challenge %q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}

	request.Header.Set("Authorization", "Bearer token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorized request = %d", response.StatusCode)
	}

	invalidTokenServer := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{ResourceAuth: &auth.ResourceServer{
		Validator: resourceValidator{err: fmt.Errorf("invalid token")},
	}}))
	defer invalidTokenServer.Close()
	request, err = http.NewRequest(http.MethodPost, invalidTokenServer.URL+"/api/v1/repositories/stable/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(response.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("invalid token = %d, challenge %q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}

	insufficientScopeServer := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{ResourceAuth: &auth.ResourceServer{
		Validator: resourceValidator{claims: auth.Claims{Scopes: []string{"package:remove"}}},
	}}))
	defer insufficientScopeServer.Close()
	request, err = http.NewRequest(http.MethodPost, insufficientScopeServer.URL+"/api/v1/repositories/stable/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	challenge := response.Header.Get("WWW-Authenticate")
	if response.StatusCode != http.StatusForbidden || !strings.Contains(challenge, `error="insufficient_scope"`) || !strings.Contains(challenge, `scope="package:publish"`) {
		t.Fatalf("insufficient scope = %d, challenge %q", response.StatusCode, challenge)
	}
	request.Header.Set("Authorization", "Bearer")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(response.Header.Get("WWW-Authenticate"), `error="invalid_request"`) {
		t.Fatalf("malformed token = %d, challenge %q", response.StatusCode, response.Header.Get("WWW-Authenticate"))
	}
}

func TestCIMDMetadata(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(service, "https://packages.example/pkgdepot")
	request := httptest.NewRequest(http.MethodGet, "/oauth/client-metadata.json", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ClientID != "https://packages.example/pkgdepot/oauth/client-metadata.json" {
		t.Fatalf("client_id = %q", metadata.ClientID)
	}
	if len(metadata.RedirectURIs) != 5 {
		t.Fatalf("redirect_uris = %v", metadata.RedirectURIs)
	}
}

func TestResourceMetadataUsesRFC9728PathForCanonicalURL(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(service, "https://packages.example/pkgdepot", httpapi.Options{ResourceAuth: &auth.ResourceServer{
		Validator: resourceValidator{},
		Metadata:  auth.ResourceMetadata{Resource: "https://packages.example/pkgdepot"},
	}})
	request := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/pkgdepot", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", response.Code)
	}
}

func TestMutationAuthorizationRequiresRoleMapping(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	roleScopes := map[string][]string{"publisher": {auth.ScopePublish}}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{
		ResourceAuth: &auth.ResourceServer{
			Validator: resourceValidator{claims: auth.Claims{Roles: []string{"publisher"}, Scopes: []string{auth.ScopePublish}}},
			Authorize: func(claims auth.Claims, scope, _, _ string) bool {
				return auth.AuthorizeRoles(claims, scope, roleScopes, "")
			},
			Metadata: auth.ResourceMetadata{
				Resource:               "http://localhost:8080",
				BearerMethodsSupported: []string{"header"},
			},
		},
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/repositories/stable/x86_64/packages/example", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("permission status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/api/v1/repositories/testing/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		t.Fatalf("publish status = %d, expected authorized", response.StatusCode)
	}
}

func TestMutationAuthorizationAllowsClientCredentialsScope(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	roleScopes := map[string][]string{"publisher": {auth.ScopePublish}}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{
		ResourceAuth: &auth.ResourceServer{
			Validator: resourceValidator{claims: auth.Claims{Scopes: []string{"package:publish"}, Subject: "client-app", ClientID: "app"}},
			Authorize: func(claims auth.Claims, scope, _, _ string) bool {
				return auth.AuthorizeRoles(claims, scope, roleScopes, "client-{client_id}")
			},
			Metadata: auth.ResourceMetadata{
				Resource:               "http://localhost:8080",
				BearerMethodsSupported: []string{"header"},
			},
		},
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/repositories/testing/x86_64/packages", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		t.Fatalf("scope-only status = %d, expected authorized", response.StatusCode)
	}
}

func TestPublishStreamsMultipartPartsToDataRoot(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	packagePart, err := writer.CreateFormFile("package", "example-1-1-x86_64.pkg.tar")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packagePart.Write(buildPackage(t, "x86_64")); err != nil {
		t.Fatal(err)
	}
	signaturePart, err := writer.CreateFormFile("signature", "example.sig")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signaturePart.Write([]byte("signature")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{
		ResourceAuth: &auth.ResourceServer{
			Validator: resourceValidator{},
			Authorize: func(claims auth.Claims, scope, _, _ string) bool {
				return auth.AuthorizeRoles(claims, scope, map[string][]string{"publisher": {auth.ScopePublish}}, "")
			},
			Metadata: auth.ResourceMetadata{
				Resource:               "http://localhost:8080",
				BearerMethodsSupported: []string{"header"},
			},
		},
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/repositories/stable/x86_64/packages", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("publish status = %d, body = %s", response.StatusCode, responseBody)
	}

	packagePath := filepath.Join(root, "repositories", "stable", "x86_64", "example-1-1-x86_64.pkg.tar")
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatal(err)
	}
	storedSignature, err := os.ReadFile(packagePath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	if string(storedSignature) != "signature" {
		t.Fatalf("stored signature = %q", storedSignature)
	}
	entries, err := os.ReadDir(filepath.Join(root, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging directory contains %d entries after publish", len(entries))
	}
}

func TestListRepositoriesIsPublic(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "repositories", "stable", "x86_64")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "stable.db.tar.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/repositories")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type = %q", contentType)
	}
	var repositories []repository.Repository
	if err := json.NewDecoder(response.Body).Decode(&repositories); err != nil {
		t.Fatal(err)
	}
	want := []repository.Repository{{Name: "stable", Architectures: []string{"x86_64"}}}
	if !reflect.DeepEqual(repositories, want) {
		t.Fatalf("repositories = %#v, want %#v", repositories, want)
	}
}

func TestRepositoryIndex(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]string{
		{"testing", "x86_64"},
		{"stable", "x86_64"},
		{"stable", "aarch64"},
	} {
		directory := filepath.Join(root, "repositories", target[0], target[1])
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, target[0]+".db.tar.gz"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "repositories", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"<title>Repositories - PKGdepot</title>",
		">PKGdepot</a>",
		"<h1>Repositories</h1>",
		`href="/repositories/stable">stable</a>`,
		`<span class="architecture font-monospace">aarch64</span>`,
		`<span class="architecture font-monospace">x86_64</span>`,
		`href="/repositories/testing">testing</a>`,
		`href="/repositories/empty">empty</a>`,
		"Empty, ready to publish",
	} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("response does not contain %q", expected)
		}
	}
	page := string(body)
	if strings.Index(page, ">stable</a>") > strings.Index(page, ">testing</a>") {
		t.Fatalf("repositories are not in name order: %q", page)
	}
	if strings.Index(page, ">aarch64</span>") > strings.Index(page, ">x86_64</span>") {
		t.Fatalf("architectures are not in name order: %q", page)
	}
	if strings.Contains(page, `/repositories/stable/x86_64`) {
		t.Fatalf("architectures remain navigation links: %q", page)
	}
}

func TestRepositoryIndexUsesConfiguredAppName(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080", httpapi.Options{AppName: "My packages"}))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, expected := range []string{"<title>Repositories - My packages</title>", ">My packages</a>"} {
		if !strings.Contains(page, expected) {
			t.Errorf("response does not contain %q", expected)
		}
	}
}

func TestPackageIndex(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		architecture string
		description  string
	}{
		{architecture: "aarch64", description: "%FILENAME%\nexample-2-1-aarch64.pkg.tar.zst\n\n%NAME%\nexample\n\n%VERSION%\n2-1\n\n%ARCH%\naarch64\n\n%DESC%\nExample package\n\n%CSIZE%\n84\n"},
		{architecture: "x86_64", description: "%FILENAME%\nexample-1-1-x86_64.pkg.tar.zst\n\n%NAME%\nexample\n\n%VERSION%\n1-1\n\n%ARCH%\nx86_64\n\n%DESC%\nExample package\n\n%CSIZE%\n42\n"},
	} {
		directory := filepath.Join(root, "repositories", "stable", target.architecture)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeDatabase(t, filepath.Join(directory, "stable.db.tar.gz"), target.description)
	}
	server := httptest.NewServer(httpapi.New(service, "http://packages.example"))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/repositories/stable", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "attacker.example"
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`<h1 class="font-monospace">stable</h1>`,
		`<strong>Add to <code>/etc/pacman.conf</code></strong>`,
		`[stable]`,
		`Server = http://packages.example/repos/stable/$arch`,
		`<td><a class="font-monospace" href="/repositories/stable/aarch64/packages/example">example</a></td>`,
		"<th>Version</th>",
		"<th>Architectures</th>",
		`<span class="architecture font-monospace">aarch64</span>`,
		`<span class="architecture font-monospace">x86_64</span>`,
		`<li class="font-monospace variant-version">2-1</li>`,
		`<li class="font-monospace variant-version">1-1</li>`,
		"<td>Example package</td>",
	} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("response does not contain %q", expected)
		}
	}
	if strings.Count(string(body), `href="/repositories/stable/aarch64/packages/example"`) != 1 {
		t.Fatalf("package was not grouped into one row: %q", body)
	}
	if strings.Contains(string(body), "42 bytes") || strings.Contains(string(body), "84 bytes") {
		t.Fatalf("package listing contains artifact sizes: %q", body)
	}
	response, err = http.Get(server.URL + "/repositories/stable/packages/example")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("non-canonical package details status = %d", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/repositories/stable/aarch64/packages/example")
	if err != nil {
		t.Fatal(err)
	}
	details, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(details), "example-2-1-aarch64.pkg.tar.zst") || strings.Contains(string(details), "example-1-1-x86_64.pkg.tar.zst") {
		t.Fatalf("architecture package details contain unexpected variants: %q", details)
	}
	response, err = http.Get(server.URL + "/repositories/stable/x86_64")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("removed architecture route status = %d", response.StatusCode)
	}
}

func TestPackageIndexUsesRepositoryArchitectureForAnyPackage(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "repositories", "stable", "aarch64")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDatabase(t, filepath.Join(directory, "stable.db.tar.gz"),
		"%FILENAME%\nuniversal-1-1-any.pkg.tar.zst\n\n%NAME%\nuniversal\n\n%VERSION%\n1-1\n\n%ARCH%\nany\n\n%DESC%\nUniversal package\n\n%CSIZE%\n42\n",
	)
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/repositories/stable")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, expected := range []string{
		`<span class="architecture font-monospace">aarch64</span>`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("response does not contain %q", expected)
		}
	}
	if strings.Contains(page, "/repositories/stable/any/") {
		t.Fatalf("package metadata architecture used as details target: %q", page)
	}
}

func TestPackageIndexFiltersAndPreservesOrder(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "repositories", "stable", "x86_64")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDatabase(t, filepath.Join(directory, "stable.db.tar.gz"),
		"%FILENAME%\nbravo-1-1-x86_64.pkg.tar.zst\n\n%NAME%\nbravo\n\n%VERSION%\n1-1\n\n%DESC%\nGraphics viewer\n\n%CSIZE%\n1536\n",
		"%FILENAME%\nalpha-1-1-x86_64.pkg.tar.zst\n\n%NAME%\nalpha\n\n%VERSION%\n1-1\n\n%DESC%\nCommand-line utility\n\n%CSIZE%\n2048\n",
	)
	aarch64Directory := filepath.Join(root, "repositories", "stable", "aarch64")
	if err := os.MkdirAll(aarch64Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDatabase(t, filepath.Join(aarch64Directory, "stable.db.tar.gz"),
		"%FILENAME%\nalpha-1-1-aarch64.pkg.tar.zst\n\n%NAME%\nalpha\n\n%VERSION%\n1-1\n\n%DESC%\nAlternate platform utility\n\n%CSIZE%\n2048\n",
	)
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/repositories/stable")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if strings.Index(page, ">alpha</a>") > strings.Index(page, ">bravo</a>") {
		t.Fatalf("packages are not in name order: %q", page)
	}
	for _, test := range []struct {
		query   string
		present string
		absent  string
	}{
		{query: "ALPHA", present: ">alpha</a>", absent: ">bravo</a>"},
		{query: "graphics", present: ">bravo</a>", absent: ">alpha</a>"},
		{query: "alternate", present: ">alpha</a>", absent: ">bravo</a>"},
	} {
		response, err := http.Get(server.URL + "/repositories/stable?q=" + test.query)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		page := string(body)
		if !strings.Contains(page, test.present) || strings.Contains(page, test.absent) {
			t.Errorf("query %q returned unexpected packages: %q", test.query, page)
		}
		if !strings.Contains(page, `value="`+test.query+`"`) {
			t.Errorf("query %q is not preserved in the search form: %q", test.query, page)
		}
	}

	response, err = http.Get(server.URL + "/repositories/stable?q=missing")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "No packages match <strong>missing</strong>.") {
		t.Fatalf("missing search result empty state not rendered: %q", body)
	}
}

func TestPackageLinksAndDetails(t *testing.T) {
	root := t.TempDir()
	service := repository.New(root, commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "repositories", "stable", "x86_64")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	filename := "example build#1.pkg.tar.zst"
	description := "%FILENAME%\n" + filename + "\n\n%NAME%\nexample\n\n%VERSION%\n1-1\n\n%ARCH%\nx86_64\n\n%DESC%\nExample package\n\n%CSIZE%\n42\n\n%DEPENDS%\nglibc\ncurl\n"
	writeDatabase(t, filepath.Join(directory, "stable.db.tar.gz"), description)
	if err := os.WriteFile(filepath.Join(directory, filename+".sig"), []byte("signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/repositories/stable")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`href="/repositories/stable/x86_64/packages/example"`,
	} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("package index does not contain %q", expected)
		}
	}
	if strings.Contains(string(body), ".pkg.tar.zst.sig") {
		t.Fatalf("package listing contains signature download: %q", body)
	}

	response, err = http.Get(server.URL + "/repositories/stable/x86_64/packages/example")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`<h1 class="font-monospace">example</h1>`, "Example package", "<code>glibc</code>", "<code>curl</code>", "Download signature"} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("package details do not contain %q", expected)
		}
	}
	for _, expected := range []string{
		`href="/repos/stable/x86_64/example%20build%231.pkg.tar.zst"`,
		`href="/repos/stable/x86_64/example%20build%231.pkg.tar.zst.sig"`,
	} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("package details do not contain %q", expected)
		}
	}
	if err := os.Remove(filepath.Join(directory, filename+".sig")); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/repositories/stable/x86_64/packages/example")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Download signature") || strings.Contains(string(body), filename+".sig") {
		t.Fatalf("unsigned package details contain a signature link: %q", body)
	}

	response, err = http.Get(server.URL + "/repositories/stable/packages/missing")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing package status = %d", response.StatusCode)
	}
}

func TestPackageIndexEmptyState(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/repositories/stable")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "No packages available.") {
		t.Fatalf("response = %q", body)
	}
}

func TestRepositoryIndexEmptyState(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "No repositories available.") {
		t.Fatalf("response = %q", body)
	}
}

func TestWebAssetsAndUnknownRoutes(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	response, err := http.Get(server.URL + "/assets/pure-min.css")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
		t.Fatalf("asset content type = %q", contentType)
	}

	response, err = http.Get(server.URL + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", response.StatusCode)
	}
}

func TestAPIAndDownloadErrorsDoNotRenderHTML(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "http://localhost:8080"))
	defer server.Close()

	for _, path := range []string{
		"/api/v1/not-found",
		"/repos/stable/x86_64",
		"/repos/stable/x86_64/missing.pkg.tar.zst",
	} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.StatusCode, http.StatusNotFound)
		}
		if contentType := response.Header.Get("Content-Type"); strings.HasPrefix(contentType, "text/html") {
			t.Errorf("GET %s content type = %q", path, contentType)
		}
		if strings.Contains(string(body), "<h1>Repositories</h1>") {
			t.Errorf("GET %s rendered the repository index: %q", path, body)
		}
	}
}

func writeDatabase(t *testing.T, path string, descriptions ...string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	archive := tar.NewWriter(gzipWriter)
	for index, description := range descriptions {
		contents := []byte(description)
		if err := archive.WriteHeader(&tar.Header{Name: fmt.Sprintf("package-%d/desc", index), Mode: 0o644, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func buildPackage(t *testing.T, architecture string) []byte {
	t.Helper()
	var packageArchive bytes.Buffer
	archive := tar.NewWriter(&packageArchive)
	metadata := []byte("pkgname = example\npkgver = 1-1\narch = " + architecture + "\n")
	if err := archive.WriteHeader(&tar.Header{Name: ".PKGINFO", Mode: 0o644, Size: int64(len(metadata))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(metadata); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return packageArchive.Bytes()
}
