package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/trly/pkgdepot/internal/oauthcache"
)

func TestClientCredentialsRequestsOperationScopeAndUsesBearerTransport(t *testing.T) {
	var scope, resource, authorization, clientID, clientSecret string
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			clientID, clientSecret, _ = r.BasicAuth()
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			scope = r.Form.Get("scope")
			resource = r.Form.Get("resource")
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			authorization = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if scope != "package:remove" {
		t.Fatalf("scope = %q, want package:remove", scope)
	}
	if resource != server.URL {
		t.Fatalf("resource = %q, want %q", resource, server.URL)
	}
	if clientID != "client" || clientSecret != "secret" {
		t.Fatalf("client credentials = %q/%q, want client/secret", clientID, clientSecret)
	}
	if authorization != "Bearer access" {
		t.Fatalf("Authorization = %q, want Bearer access", authorization)
	}
}

func TestAuthorizationCodeUsesPKCEAndLoopbackCallback(t *testing.T) {
	var authorizationRequest url.Values
	var tokenRequest url.Values
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			tokenRequest = r.Form
			writeJSON(w, `{"access_token":"delegated-access","token_type":"Bearer","refresh_token":"refresh","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			if r.Header.Get("Authorization") != "Bearer delegated-access" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Errorf("parse authorization URL: %v", err)
			return
		}
		authorizationRequest = parsed.Query()
		callback, err := url.Parse(authorizationRequest.Get("redirect_uri"))
		if err != nil {
			t.Errorf("parse redirect URI: %v", err)
			return
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {authorizationRequest.Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Errorf("send OAuth callback: %v", err)
			return
		}
		_ = response.Body.Close()
	}
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if authorizationRequest.Get("response_type") != "code" {
		t.Fatalf("response_type = %q, want code", authorizationRequest.Get("response_type"))
	}
	redirectURI := authorizationRequest.Get("redirect_uri")
	parsedRedirect, err := url.Parse(redirectURI)
	if err != nil {
		t.Fatalf("parse redirect_uri: %v", err)
	}
	if parsedRedirect.Hostname() != "127.0.0.1" || parsedRedirect.Path != "/oauth/callback" {
		t.Fatalf("redirect_uri = %q, want loopback address", redirectURI)
	}
	port := 0
	for _, p := range httpclient.LoopbackPorts {
		if fmt.Sprintf("%d", p) == parsedRedirect.Port() {
			port = p
		}
	}
	if port == 0 {
		t.Fatalf("redirect_uri port %s not in LoopbackPorts %v", parsedRedirect.Port(), httpclient.LoopbackPorts)
	}
	if authorizationRequest.Get("scope") != "openid package:remove" || authorizationRequest.Get("resource") != server.URL {
		t.Fatalf("authorization request = %v", authorizationRequest)
	}
	if authorizationRequest.Get("code_challenge") == "" || authorizationRequest.Get("code_challenge_method") != "S256" || authorizationRequest.Get("state") == "" {
		t.Fatalf("authorization request is missing PKCE or state: %v", authorizationRequest)
	}
	if tokenRequest.Get("grant_type") != "authorization_code" || tokenRequest.Get("code") != "authorization-code" || tokenRequest.Get("code_verifier") == "" || tokenRequest.Get("resource") != server.URL {
		t.Fatalf("token request = %v", tokenRequest)
	}
}

func TestAuthorizationCodeUsesCIMDClientIDByDefault(t *testing.T) {
	t.Setenv("PKGDEPOT_OAUTH_CLIENT_ID", "")
	var authorizationRequest url.Values
	server := oauthServerWithCIMD(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			t.Fatal("authorization endpoint must not be called directly")
		case "/token":
			writeJSON(w, `{"access_token":"delegated-access","token_type":"Bearer","expires_in":3600}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.HTTP = server.Client()
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		authorizationRequest = parsed.Query()
		callback, err := url.Parse(authorizationRequest.Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {authorizationRequest.Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err == nil {
		t.Fatal("Remove() unexpectedly succeeded")
	}
	if authorizationRequest.Get("client_id") != server.URL+"/oauth/client-metadata.json" {
		t.Fatalf("client_id = %q", authorizationRequest.Get("client_id"))
	}
}

func TestAuthorizationCodeHTTPResourceRequiresConfiguredClientID(t *testing.T) {
	t.Setenv("PKGDEPOT_OAUTH_CLIENT_ID", "")
	server := oauthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if err == nil || !strings.Contains(err.Error(), "use an HTTPS PKGDEPOT_URL or set PKGDEPOT_OAUTH_CLIENT_ID") {
		t.Fatalf("Remove() error = %v, want actionable HTTP resource error", err)
	}
}

func TestDelegatedLoginPersistsTokenForLaterClient(t *testing.T) {
	backend := &memoryTokenBackend{values: make(map[string]string)}
	var refreshResource string
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshResource = r.Form.Get("resource")
				writeJSON(w, `{"access_token":"refreshed","token_type":"Bearer","refresh_token":"refresh","expires_in":3600}`)
				return
			}
			writeJSON(w, `{"access_token":"delegated","token_type":"Bearer","refresh_token":"refresh","expires_in":-3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	first := httpclient.New(context.Background(), server.URL)
	first.OAuth.ClientID = "public-client"
	first.SetTokenStore(oauthcache.NewWithBackend(backend))
	first.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {parsed.Query().Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if _, err := first.Login(context.Background(), []string{"package:publish", "package:remove"}); err != nil {
		t.Fatal(err)
	}

	second := httpclient.New(context.Background(), server.URL)
	second.OAuth.ClientID = "public-client"
	second.SetTokenStore(oauthcache.NewWithBackend(backend))
	if err := second.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if refreshResource != server.URL {
		t.Fatalf("refresh resource = %q, want %q", refreshResource, server.URL)
	}
}

func TestAuthorizationCodeTokenSourceRefreshIncludesResource(t *testing.T) {
	var refreshResource string
	var tokenCount atomic.Int32
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshResource = r.Form.Get("resource")
				writeJSON(w, `{"access_token":"refreshed","token_type":"Bearer","refresh_token":"refresh2","expires_in":3600}`)
				return
			}
			if tokenCount.Add(1) == 1 {
				writeJSON(w, `{"access_token":"initial","token_type":"Bearer","refresh_token":"refresh1","expires_in":-3600}`)
				return
			}
			writeJSON(w, `{"access_token":"fresh","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {parsed.Query().Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if refreshResource != server.URL {
		t.Fatalf("refresh resource = %q, want %q", refreshResource, server.URL)
	}
}

func TestExpiredCachedTokenWithoutRefreshTokenStartsNewLogin(t *testing.T) {
	var authorizationCount atomic.Int32
	var tokenCount atomic.Int32
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if tokenCount.Add(1) == 1 {
				writeJSON(w, `{"access_token":"expired","token_type":"Bearer","expires_in":-3600}`)
				return
			}
			writeJSON(w, `{"access_token":"fresh","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	backend := &memoryTokenBackend{values: make(map[string]string)}
	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(backend))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		authorizationCount.Add(1)
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {parsed.Query().Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if _, err := client.Login(context.Background(), []string{"package:remove"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
	if authorizationCount.Load() != 2 {
		t.Fatalf("authorization count = %d, want 2", authorizationCount.Load())
	}
}

type memoryTokenBackend struct{ values map[string]string }

func (m *memoryTokenBackend) Get(_, user string) (string, error) {
	value, ok := m.values[user]
	if !ok {
		return "", oauthcache.ErrNotFound
	}
	return value, nil
}

func (m *memoryTokenBackend) Set(_, user, value string) error {
	m.values[user] = value
	return nil
}

func (m *memoryTokenBackend) Delete(_, user string) error {
	delete(m.values, user)
	return nil
}

func TestAuthorizationCodeCancellationClosesLoopbackListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	client := httpclient.New(ctx, server.URL)
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))
	client.OAuth.ClientID = "public-client"
	client.OAuth.AuthorizationPrompt = func(string) { cancel() }
	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Remove() error = %v, want context.Canceled", err)
	}
}

func TestConcurrentRequestsReuseClientCredentialsToken(t *testing.T) {
	var tokenRequests atomic.Int32
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
				t.Errorf("Remove() error = %v", err)
			}
		}()
	}
	group.Wait()
	if tokenRequests.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests.Load())
	}
}

func TestDiscoverySelectsExpectedIssuer(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_, _ = io.WriteString(w, `{"resource":"`+server.URL+`","authorization_servers":["`+server.URL+`/other","`+server.URL+`/expected"]}`)
		case "/expected/.well-known/openid-configuration":
			_, _ = io.WriteString(w, `{"issuer":"`+server.URL+`/expected","token_endpoint":"`+server.URL+`/token","token_endpoint_auth_methods_supported":["client_secret_basic"]}`)
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	client.OAuth.ExpectedIssuer = server.URL + "/expected"
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
}

func TestClientCredentialsRequireIssuerBeforeDiscovery(t *testing.T) {
	requests := 0
	client := httpclient.New(context.Background(), "https://packages.example")
	client.OAuth.ClientID = "client"
	client.OAuth.ClientSecret = "secret"
	client.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected request")
	})}

	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if err == nil || !strings.Contains(err.Error(), "PKGDEPOT_OAUTH_ISSUER") {
		t.Fatalf("Remove() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("discovery requests = %d, want 0", requests)
	}
}

func TestClientCredentialsRequireBasicAuthenticationSupport(t *testing.T) {
	var tokenRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			writeJSON(w, `{"resource":"`+server.URL+`","authorization_servers":["`+server.URL+`"]}`)
		case "/.well-known/openid-configuration":
			writeJSON(w, `{"issuer":"`+server.URL+`","token_endpoint":"`+server.URL+`/token","token_endpoint_auth_methods_supported":["client_secret_post"]}`)
		case "/token":
			tokenRequests++
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if err == nil || !strings.Contains(err.Error(), "does not support client_secret_basic") {
		t.Fatalf("Remove() error = %v", err)
	}
	if tokenRequests != 0 {
		t.Fatalf("token requests = %d, want 0", tokenRequests)
	}
}

func TestClientCredentialsAcceptsOmittedAuthMethods(t *testing.T) {
	var tokenRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			writeJSON(w, `{"resource":"`+server.URL+`","authorization_servers":["`+server.URL+`"]}`)
		case "/.well-known/openid-configuration":
			writeJSON(w, `{"issuer":"`+server.URL+`","token_endpoint":"`+server.URL+`/token"}`)
		case "/token":
			tokenRequests++
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
}

func TestClientRejectsNonLoopbackHTTPResource(t *testing.T) {
	client := httpclient.New(context.Background(), "http://packages.example")
	if _, err := client.List(context.Background(), "stable", "x86_64"); err == nil {
		t.Fatal("accepted non-loopback HTTP resource URL")
	}
}

func TestDiscoveryRejectsMismatchedResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			writeJSON(w, `{"resource":"https://other.example","authorization_servers":["`+r.Host+`"]}`)
			return
		}
		t.Fatal("unexpected OIDC request")
	}))
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err == nil || !strings.Contains(err.Error(), "does not match requested resource") {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestListDoesNotDiscoverOAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			t.Fatal("unexpected discovery request")
		}
		writeJSON(w, `[]`)
	}))
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	packages, err := client.List(context.Background(), "stable", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 0 {
		t.Fatalf("packages = %v, want empty", packages)
	}
}

func TestPublishReportsEarlyServerResponse(t *testing.T) {
	packagePath := t.TempDir() + "/example.pkg.tar.zst"
	if err := os.WriteFile(packagePath, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		default:
			if r.Body != nil {
				_ = r.Body.Close()
			}
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, `{"error":"route not found","code":"not_found"}`)
		}
	})
	defer server.Close()

	client := authenticatedClient(context.Background(), server.URL)
	_, err := client.Publish(context.Background(), "stable", "x86_64", packagePath, "")
	if err == nil || err.Error() != "server returned 404 Not Found: route not found" {
		t.Fatalf("Publish() error = %v", err)
	}
	var apiError *httpclient.APIError
	if !errors.As(err, &apiError) || apiError.Code != "not_found" {
		t.Fatalf("Publish() error code = %#v, want not_found", apiError)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func authenticatedClient(ctx context.Context, baseURL string) *httpclient.Client {
	client := httpclient.New(ctx, baseURL)
	client.OAuth.ClientID = "client"
	client.OAuth.ClientSecret = "secret"
	client.OAuth.ExpectedIssuer = baseURL
	return client
}

func oauthServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_, _ = io.WriteString(w, `{"resource":"`+server.URL+`","authorization_servers":["`+server.URL+`"]}`)
		case "/.well-known/openid-configuration":
			_, _ = io.WriteString(w, `{"issuer":"`+server.URL+`","authorization_endpoint":"`+server.URL+`/authorize","token_endpoint":"`+server.URL+`/token","token_endpoint_auth_methods_supported":["client_secret_basic"]}`)
		default:
			handler(w, r)
		}
	}))
	return server
}

func oauthServerWithCIMD(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_, _ = io.WriteString(w, `{"resource":"`+server.URL+`","authorization_servers":["`+server.URL+`"]}`)
		case "/.well-known/openid-configuration":
			_, _ = io.WriteString(w, `{"issuer":"`+server.URL+`","authorization_endpoint":"`+server.URL+`/authorize","token_endpoint":"`+server.URL+`/token","token_endpoint_auth_methods_supported":["none"],"client_id_metadata_document_supported":true}`)
		case "/oauth/client-metadata.json":
			_, _ = io.WriteString(w, `{"client_id":"`+server.URL+`/oauth/client-metadata.json","redirect_uris":["http://127.0.0.1:8085/oauth/callback"],"token_endpoint_auth_method":"none"}`)
		default:
			handler(w, r)
		}
	}))
	return server
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func TestLoopbackRedirectURLsCoversAllPorts(t *testing.T) {
	urls := httpclient.LoopbackRedirectURLs()
	if len(urls) != len(httpclient.LoopbackPorts) {
		t.Fatalf("LoopbackRedirectURLs() returned %d URLs, want %d", len(urls), len(httpclient.LoopbackPorts))
	}
	for i, port := range httpclient.LoopbackPorts {
		want := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", port)
		if urls[i] != want {
			t.Fatalf("LoopbackRedirectURLs()[%d] = %q, want %q", i, urls[i], want)
		}
	}
}

func TestRepositoryClientLifecycleMethods(t *testing.T) {
	var requests []string
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/token":
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories":
			writeJSON(w, `[{"name":"stable","architectures":[]}]`)
		case "/api/v1/repositories/stable":
			if r.Method == http.MethodPatch {
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"name":"testing"}` {
					t.Errorf("rename body = %s", body)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()
	client := authenticatedClient(context.Background(), server.URL)

	repositories, err := client.Repositories(context.Background())
	if err != nil || len(repositories) != 1 || repositories[0].Name != "stable" {
		t.Fatalf("Repositories() = %#v, %v", repositories, err)
	}
	if err := client.CreateRepository(context.Background(), "stable"); err != nil {
		t.Fatal(err)
	}
	if err := client.RenameRepository(context.Background(), "stable", "testing"); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveRepository(context.Background(), "stable"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GET /api/v1/repositories", "POST /api/v1/repositories/stable", "PATCH /api/v1/repositories/stable", "DELETE /api/v1/repositories/stable"} {
		if !containsRequest(requests, want) {
			t.Fatalf("requests = %v, missing %q", requests, want)
		}
	}
}

func containsRequest(requests []string, want string) bool {
	for _, request := range requests {
		if request == want {
			return true
		}
	}
	return false
}

func TestAuthorizationCodeFailsWhenAllLoopbackPortsOccupied(t *testing.T) {
	holders := make([]net.Listener, len(httpclient.LoopbackPorts))
	for i, port := range httpclient.LoopbackPorts {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("occupy port %d: %v", port, err)
		}
		holders[i] = l
	}
	defer func() {
		for _, l := range holders {
			_ = l.Close()
		}
	}()

	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))
	client.OAuth.ClientID = "public-client"
	err := client.Remove(context.Background(), "stable", "x86_64", "example")
	if err == nil || !strings.Contains(err.Error(), "all loopback ports") {
		t.Fatalf("Remove() error = %v, want all-loopback-ports error", err)
	}
}

func TestLoginPropagatesIdentityAuthorizationError(t *testing.T) {
	server := oauthServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"

	_, err := client.Login(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() error = %v, want context.Canceled", err)
	}
}

func TestLoginCachesGrantedScopesNotRequestedScopes(t *testing.T) {
	backend := &memoryTokenBackend{values: make(map[string]string)}
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, `{"access_token":"token","token_type":"Bearer","scope":"package:publish","expires_in":3600}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(backend))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {parsed.Query().Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if _, err := client.Login(context.Background(), []string{"package:publish", "package:remove"}); err != nil {
		t.Fatal(err)
	}

	store := oauthcache.NewWithBackend(backend)
	record, err := store.Get(server.URL + "\x00" + "public-client" + "\x00" + server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Scopes) != 1 || record.Scopes[0] != "package:publish" {
		t.Fatalf("cached scopes = %v, want [package:publish]", record.Scopes)
	}
}

func TestLoginFallsBackToRequestedScopesWhenResponseOmitsScope(t *testing.T) {
	backend := &memoryTokenBackend{values: make(map[string]string)}
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(backend))
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		callback, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callback.RawQuery = url.Values{"code": {"authorization-code"}, "state": {parsed.Query().Get("state")}}.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if _, err := client.Login(context.Background(), []string{"package:publish", "package:remove"}); err != nil {
		t.Fatal(err)
	}

	store := oauthcache.NewWithBackend(backend)
	record, err := store.Get(server.URL + "\x00" + "public-client" + "\x00" + server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Scopes) != 2 || record.Scopes[0] != "package:publish" || record.Scopes[1] != "package:remove" {
		t.Fatalf("cached scopes = %v, want [package:publish package:remove]", record.Scopes)
	}
}

func TestInvalidStateCallbackDoesNotAbortLogin(t *testing.T) {
	server := oauthServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeJSON(w, `{"access_token":"access","token_type":"Bearer","expires_in":3600}`)
		case "/api/v1/repositories/stable/x86_64/packages/example":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	client := httpclient.New(context.Background(), server.URL)
	client.OAuth.ClientID = "public-client"
	client.SetTokenStore(oauthcache.NewWithBackend(&memoryTokenBackend{values: make(map[string]string)}))

	var callbackURL *url.URL
	client.OAuth.AuthorizationPrompt = func(authorizationURL string) {
		parsed, err := url.Parse(authorizationURL)
		if err != nil {
			t.Errorf("parse authorization URL: %v", err)
			return
		}
		callbackURL, _ = url.Parse(parsed.Query().Get("redirect_uri"))

		wrongState := callbackURL.ResolveReference(&url.URL{RawQuery: url.Values{"state": {"wrong"}, "code": {"bogus"}}.Encode()})
		resp, err := http.Get(wrongState.String())
		if err != nil {
			t.Errorf("send wrong-state callback: %v", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("wrong-state callback status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}

		state := parsed.Query().Get("state")
		goodURL := callbackURL.ResolveReference(&url.URL{RawQuery: url.Values{"state": {state}, "code": {"authorization-code"}}.Encode()})
		resp, err = http.Get(goodURL.String())
		if err != nil {
			t.Errorf("send correct callback: %v", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("correct callback status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}

	if err := client.Remove(context.Background(), "stable", "x86_64", "example"); err != nil {
		t.Fatal(err)
	}
}
