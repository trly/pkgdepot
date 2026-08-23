package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/trly/pkgdepot/internal/api"
	"github.com/trly/pkgdepot/internal/auth"
	"github.com/trly/pkgdepot/internal/cimd"
	"github.com/trly/pkgdepot/internal/oauthcache"
	"github.com/trly/pkgdepot/internal/urlpolicy"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	OAuth   OAuthOptions

	ctx context.Context

	mu             sync.Mutex
	discoverOnce   sync.Once
	discoverErr    error
	resourceURL    string
	issuer         string
	publisherID    string
	adminID        string
	activeClientID string
	endpoint       oauth2.Endpoint
	provider       *oidc.Provider
	resourceScopes []string
	tokenSources   map[string]oauth2.TokenSource
	tokenStore     *oauthcache.Store
}

var cachedTokenLocks sync.Map

// OAuthOptions controls OAuth credentials and user interaction used by Client.
// The authorization server is discovered from protected-resource metadata and
// its OpenID Connect discovery document.
type OAuthOptions struct {
	ClientID            string
	ClientSecret        string
	ExpectedIssuer      string
	Access              string
	AuthorizationPrompt func(string)
}

const (
	AccessPublisher = "publisher"
	AccessAdmin     = "admin"
)

func selectAccess(access string, scopes []string) (string, error) {
	if access == "" {
		for _, scope := range scopes {
			if scope != "package:publish" {
				return AccessAdmin, nil
			}
		}
		return AccessPublisher, nil
	}
	if access != AccessPublisher && access != AccessAdmin {
		return "", fmt.Errorf("invalid OAuth access profile %q (want publisher or admin)", access)
	}
	if access == AccessPublisher {
		for _, scope := range scopes {
			if scope != auth.ScopePublish {
				return "", fmt.Errorf("scope %q requires the admin OAuth access profile", scope)
			}
		}
	}
	return access, nil
}

func accessForScope(scope string) string {
	if scope == auth.ScopePublish {
		return AccessPublisher
	}
	return AccessAdmin
}

func (c *Client) clientIDForAccess(access string) string {
	if access == AccessAdmin {
		return c.adminID
	}
	return c.publisherID
}

func (c *Client) clientIDs() []string {
	ids := []string{c.publisherID}
	if c.adminID != c.publisherID {
		ids = append(ids, c.adminID)
	}
	return ids
}

// LoopbackRedirectURLs returns the portless redirect URIs accepted by the
// authorization server. The CLI binds an ephemeral port for each transaction.
func LoopbackRedirectURLs() []string {
	return cimd.RedirectURLs()
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// APIError preserves the server's stable error code for callers that need to
// branch on failures without parsing human-readable messages.
type APIError struct {
	Status  string
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server returned %s: %s", e.Status, e.Message)
}

func New(ctx context.Context, baseURL string) *Client {
	baseURL = normalizeResourceIdentifier(baseURL)
	return &Client{
		BaseURL: baseURL,
		HTTP:    http.DefaultClient,
		OAuth: OAuthOptions{
			ClientID:       os.Getenv("PKGDEPOT_OAUTH_CLIENT_ID"),
			ClientSecret:   os.Getenv("PKGDEPOT_OAUTH_CLIENT_SECRET"),
			ExpectedIssuer: os.Getenv("PKGDEPOT_OAUTH_ISSUER"),
			AuthorizationPrompt: func(authorizationURL string) {
				fmt.Fprintf(os.Stderr, "Open %s to authenticate.\n", authorizationURL)
			},
		},
		ctx:          ctx,
		tokenSources: make(map[string]oauth2.TokenSource),
		tokenStore:   oauthcache.New(),
	}
}

// SetTokenStore replaces the secure token store, primarily for tests.
func (c *Client) SetTokenStore(store *oauthcache.Store) {
	c.tokenStore = store
}

// Login performs a delegated authorization-code login and securely caches the
// resulting token together with the scopes requested by the caller.
func (c *Client) Login(ctx context.Context, scopes []string) (*oauth2.Token, error) {
	if c.OAuth.ClientSecret != "" {
		return nil, errors.New("delegated login requires PKGDEPOT_OAUTH_CLIENT_SECRET to be unset")
	}
	access, err := selectAccess(c.OAuth.Access, scopes)
	if err != nil {
		return nil, err
	}
	c.activeClientID = c.clientIDForAccess(access)
	if err := c.discover(); err != nil {
		return nil, err
	}
	c.activeClientID = c.clientIDForAccess(access)
	if access == AccessPublisher {
		c.resourceScopes = []string{auth.ScopePublish}
	}
	identityConfig := oauth2.Config{ClientID: c.activeClientID, Endpoint: c.endpoint, Scopes: []string{"openid", "profile", "email"}}
	identity, err := c.authorizeCode(ctx, identityConfig, "", true)
	if err != nil {
		return nil, err
	}
	if user, verifyErr := c.identity(identity.token, identity.nonce); verifyErr == nil {
		selected, selectErr := c.selectScopes(ctx, user, scopes)
		if selectErr != nil {
			return nil, selectErr
		}
		scopes = selected
	} else if !strings.Contains(verifyErr.Error(), "did not return an OIDC ID token") {
		return nil, verifyErr
	}
	if len(scopes) == 0 {
		return nil, errors.New("at least one OAuth scope must be selected")
	}
	config := oauth2.Config{
		ClientID: c.activeClientID,
		Endpoint: c.endpoint,
		Scopes:   append([]string(nil), scopes...),
	}
	authContext := context.WithValue(ctx, oauth2.HTTPClient, c.HTTP)
	token, err := (&authorizationCodeTokenSource{
		ctx:                 authContext,
		config:              config,
		client:              c,
		resource:            c.resourceURL,
		authorizationPrompt: c.OAuth.AuthorizationPrompt,
	}).authorize()
	if err != nil {
		return nil, err
	}
	if err := c.saveToken(token, grantedScopes(token, scopes)); err != nil {
		return nil, err
	}
	return token, nil
}

func (c *Client) Logout() error {
	if err := c.discover(); err != nil {
		return err
	}
	if c.tokenStore == nil {
		return nil
	}
	for _, clientID := range c.clientIDs() {
		if err := c.tokenStore.Delete(c.cacheKey(clientID)); err != nil && !errors.Is(err, oauthcache.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, repository, architecture, packagePath, signaturePath string) (api.Package, error) {
	endpoint, err := c.packagesURL(repository, architecture)
	if err != nil {
		return api.Package{}, err
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request, err := c.request(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return api.Package{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	writeResult := make(chan error, 1)
	go func() {
		err := addFile(multipartWriter, "package", packagePath)
		if err == nil && signaturePath != "" {
			err = addFile(multipartWriter, "signature", signaturePath)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = writer.CloseWithError(err)
		writeResult <- err
	}()

	client, err := c.authorizedClient("package:publish")
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return api.Package{}, err
	}
	var pkg api.Package
	requestErr := c.do(client, request, &pkg)
	writeErr := <-writeResult
	if writeErr != nil && (requestErr == nil || !errors.Is(writeErr, io.ErrClosedPipe)) {
		return api.Package{}, writeErr
	}
	if requestErr != nil {
		return api.Package{}, requestErr
	}
	return pkg, nil
}

func (c *Client) List(ctx context.Context, repository, architecture string) ([]api.Package, error) {
	const pageSize = 100
	var all []api.Package
	for offset := 0; ; offset += pageSize {
		endpoint, err := c.packagesURL(repository, architecture)
		if err != nil {
			return nil, err
		}
		query := endpoint.Query()
		query.Set("limit", strconv.Itoa(pageSize))
		query.Set("cursor", strconv.Itoa(offset))
		endpoint.RawQuery = query.Encode()
		request, err := c.request(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		var page []api.Package
		if err := c.do(c.HTTP, request, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func (c *Client) Repositories(ctx context.Context) ([]api.Repository, error) {
	endpoint, err := c.repositoriesURL()
	if err != nil {
		return nil, err
	}
	request, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var repositories []api.Repository
	if err := c.do(c.HTTP, request, &repositories); err != nil {
		return nil, err
	}
	return repositories, nil
}

func (c *Client) CreateRepository(ctx context.Context, repository string) error {
	endpoint, err := c.repositoryURL(repository)
	if err != nil {
		return err
	}
	request, err := c.request(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	client, err := c.authorizedClient("repo:create")
	if err != nil {
		return err
	}
	return c.do(client, request, nil)
}

func (c *Client) RemoveRepository(ctx context.Context, repository string) error {
	endpoint, err := c.repositoryURL(repository)
	if err != nil {
		return err
	}
	request, err := c.request(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	client, err := c.authorizedClient("repo:remove")
	if err != nil {
		return err
	}
	return c.do(client, request, nil)
}

func (c *Client) RenameRepository(ctx context.Context, repository, newName string) error {
	endpoint, err := c.repositoryURL(repository)
	if err != nil {
		return err
	}
	body, err := json.Marshal(api.RenameRepositoryRequest{Name: newName})
	if err != nil {
		return fmt.Errorf("encode repository rename: %w", err)
	}
	request, err := c.request(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client, err := c.authorizedClient("repo:rename")
	if err != nil {
		return err
	}
	return c.do(client, request, nil)
}

func (c *Client) Remove(ctx context.Context, repository, architecture, packageName string) error {
	endpoint, err := c.packagesURL(repository, architecture, packageName)
	if err != nil {
		return err
	}
	request, err := c.request(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	client, err := c.authorizedClient("package:remove")
	if err != nil {
		return err
	}
	return c.do(client, request, nil)
}

func (c *Client) packagesURL(repository, architecture string, packageName ...string) (*url.URL, error) {
	if err := urlpolicy.Validate(c.BaseURL, "pkgdepot resource URL"); err != nil {
		return nil, err
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	components := []string{"api", "v1", "repositories", repository, architecture, "packages"}
	return appendURLPath(base, append(components, packageName...)...), nil
}

func (c *Client) repositoriesURL() (*url.URL, error) {
	if err := urlpolicy.Validate(c.BaseURL, "pkgdepot resource URL"); err != nil {
		return nil, err
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return appendURLPath(base, "api", "v1", "repositories"), nil
}

func (c *Client) repositoryURL(repository string) (*url.URL, error) {
	base, err := c.repositoriesURL()
	if err != nil {
		return nil, err
	}
	return appendURLPath(base, repository), nil
}

func appendURLPath(base *url.URL, components ...string) *url.URL {
	endpoint := *base
	escapedComponents := make([]string, len(components))
	for index, component := range components {
		escapedComponents[index] = url.PathEscape(component)
	}
	endpoint.Path = strings.TrimRight(base.Path, "/") + "/" + strings.Join(components, "/")
	endpoint.RawPath = strings.TrimRight(base.EscapedPath(), "/") + "/" + strings.Join(escapedComponents, "/")
	return &endpoint
}

func (c *Client) request(ctx context.Context, method string, endpoint *url.URL, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	return request, nil
}

func (c *Client) authorizedClient(scope string) (*http.Client, error) {
	source, err := c.tokenSource(scope)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(c.oauthContext(), source), nil
}

func (c *Client) tokenSource(scope string) (oauth2.TokenSource, error) {
	if err := c.discover(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	access := accessForScope(scope)
	clientID := c.clientIDForAccess(access)
	key := access + "\x00" + scope
	if source := c.tokenSources[key]; source != nil {
		return source, nil
	}

	if c.OAuth.ClientSecret != "" {
		config := clientcredentials.Config{
			ClientID:       clientID,
			ClientSecret:   c.OAuth.ClientSecret,
			TokenURL:       c.endpoint.TokenURL,
			Scopes:         []string{scope},
			AuthStyle:      oauth2.AuthStyleInHeader,
			EndpointParams: url.Values{"resource": {c.resourceURL}},
		}
		source := oauth2.ReuseTokenSourceWithExpiry(nil, config.TokenSource(c.oauthContext()), 30*time.Second)
		c.tokenSources[key] = source
		return source, nil
	}

	config := oauth2.Config{
		ClientID: clientID,
		Endpoint: c.endpoint,
		Scopes:   []string{scope},
	}
	if c.tokenStore != nil {
		cacheKey := c.cacheKey(clientID)
		record, err := c.tokenStore.Get(cacheKey)
		if errors.Is(err, oauthcache.ErrNotFound) && access == AccessPublisher && c.adminID != c.publisherID {
			clientID = c.adminID
			cacheKey = c.cacheKey(clientID)
			record, err = c.tokenStore.Get(cacheKey)
		}
		if err == nil {
			config.ClientID = clientID
			if !contains(record.Scopes, scope) {
				return nil, fmt.Errorf("cached delegated login does not include %s; run pkgdepot login to update selected scopes", scope)
			}
			source := &cachedTokenSource{
				client:   c,
				config:   config,
				clientID: clientID,
				key:      cacheKey,
				scopes:   record.Scopes,
			}
			c.tokenSources[key] = source
			return source, nil
		} else if !errors.Is(err, oauthcache.ErrNotFound) {
			return nil, fmt.Errorf("read cached delegated login: %w", err)
		}
	}
	source := oauth2.ReuseTokenSourceWithExpiry(nil, &persistingTokenSource{
		source: &authorizationCodeTokenSource{
			ctx:                 c.oauthContext(),
			config:              config,
			client:              c,
			resource:            c.resourceURL,
			authorizationPrompt: c.OAuth.AuthorizationPrompt,
		},
		client:   c,
		clientID: clientID,
		scopes:   []string{scope},
	}, 30*time.Second)
	c.tokenSources[key] = source
	return source, nil
}

func (c *Client) discover() error {
	if err := urlpolicy.Validate(c.BaseURL, "pkgdepot resource URL"); err != nil {
		return err
	}
	c.discoverOnce.Do(func() {
		c.discoverErr = c.doDiscover()
	})
	return c.discoverErr
}

func (c *Client) doDiscover() error {
	if c.OAuth.ClientSecret != "" && c.OAuth.ExpectedIssuer == "" {
		return errors.New("OAuth issuer is required for client credentials (set PKGDEPOT_OAUTH_ISSUER)")
	}

	var resource protectedResourceMetadata
	if err := c.getJSON(wellKnownURL(c.BaseURL, "oauth-protected-resource"), &resource); err != nil {
		return fmt.Errorf("discover OAuth protected resource: %w", err)
	}
	resourceURL := normalizeResourceIdentifier(resource.Resource)
	if err := urlpolicy.Validate(resourceURL, "OAuth protected resource metadata resource"); err != nil {
		return err
	}
	if resourceURL != c.BaseURL {
		return fmt.Errorf("OAuth protected resource metadata resource %q does not match requested resource %q", resource.Resource, c.BaseURL)
	}
	issuer, err := selectIssuer(resource.AuthorizationServers, c.OAuth.ExpectedIssuer)
	if err != nil {
		return err
	}
	provider, err := oidc.NewProvider(c.oauthContext(), issuer)
	if err != nil {
		return fmt.Errorf("discover OpenID Connect provider: %w", err)
	}
	endpoint := provider.Endpoint()
	var providerMetadata struct {
		Issuer                            string   `json:"issuer"`
		AuthURL                           string   `json:"authorization_endpoint"`
		TokenURL                          string   `json:"token_endpoint"`
		JWKSURL                           string   `json:"jwks_uri"`
		TokenEndpointAuthMethods          []string `json:"token_endpoint_auth_methods_supported"`
		ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	}
	if err := provider.Claims(&providerMetadata); err != nil {
		return fmt.Errorf("read OpenID Connect provider metadata: %w", err)
	}
	if err := urlpolicy.Validate(providerMetadata.Issuer, "OpenID Connect issuer"); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"OpenID Connect authorization_endpoint": providerMetadata.AuthURL,
		"OpenID Connect token_endpoint":         providerMetadata.TokenURL,
		"OpenID Connect jwks_uri":               providerMetadata.JWKSURL,
	} {
		if value != "" {
			if err := urlpolicy.ValidateEndpoint(value, name); err != nil {
				return err
			}
		}
	}
	if endpoint.TokenURL == "" {
		return errors.New("OpenID Connect provider metadata has no token_endpoint")
	}
	if c.OAuth.ClientSecret != "" && len(providerMetadata.TokenEndpointAuthMethods) > 0 && !contains(providerMetadata.TokenEndpointAuthMethods, "client_secret_basic") {
		return errors.New("OpenID Connect provider metadata does not support client_secret_basic")
	}
	if c.OAuth.ClientSecret == "" && endpoint.AuthURL == "" {
		return errors.New("OpenID Connect provider metadata has no authorization_endpoint")
	}
	clientID := c.OAuth.ClientID
	if clientID == "" {
		if c.OAuth.ClientSecret != "" {
			return errors.New("OAuth client ID is required for client credentials (set PKGDEPOT_OAUTH_CLIENT_ID)")
		}
		clientID, err = cimd.MetadataURLForPath(c.BaseURL, cimd.PublisherMetadataPath)
		if err != nil {
			return fmt.Errorf("derive CIMD client ID: %w; use an HTTPS PKGDEPOT_URL or set PKGDEPOT_OAUTH_CLIENT_ID", err)
		}
		if !providerMetadata.ClientIDMetadataDocumentSupported {
			return errors.New("OAuth provider does not support Client ID Metadata Documents; set PKGDEPOT_OAUTH_CLIENT_ID")
		}
	}

	c.resourceURL = resourceURL
	c.issuer = issuer
	if c.OAuth.ClientID != "" {
		c.publisherID, c.adminID = clientID, clientID
	} else {
		c.publisherID = clientID
		c.adminID, err = cimd.MetadataURLForPath(c.BaseURL, cimd.AdminMetadataPath)
		if err != nil {
			return fmt.Errorf("derive admin CIMD client ID: %w", err)
		}
	}
	c.endpoint = endpoint
	c.provider = provider
	c.resourceScopes = append([]string(nil), resource.ScopesSupported...)
	return nil
}

func (c *Client) oauthTokenSource(config oauth2.Config, token *oauth2.Token) oauth2.TokenSource {
	return config.TokenSource(context.WithValue(c.oauthContext(), oauth2.HTTPClient, c.resourceHTTPClient()), token)
}

func (c *Client) resourceHTTPClient() *http.Client {
	httpClient := *c.HTTP
	httpClient.Transport = &resourceIndicatorTransport{
		resource: c.resourceURL,
		base:     c.HTTP.Transport,
	}
	return &httpClient
}

type resourceIndicatorTransport struct {
	resource string
	base     http.RoundTripper
}

func (t *resourceIndicatorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Method == http.MethodPost {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		vals, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		if vals.Get("grant_type") == "refresh_token" {
			vals.Set("resource", t.resource)
			newBody := vals.Encode()
			req = req.Clone(req.Context())
			req.Body = io.NopCloser(strings.NewReader(newBody))
			req.ContentLength = int64(len(newBody))
		} else {
			req = req.Clone(req.Context())
			req.Body = io.NopCloser(strings.NewReader(string(body)))
		}
	}
	return base.RoundTrip(req)
}

func (c *Client) cacheKey(clientID string) string {
	return c.resourceURL + "\x00" + clientID + "\x00" + c.issuer
}

func (c *Client) saveToken(token *oauth2.Token, scopes []string) error {
	return c.saveTokenFor(c.activeClientID, token, scopes)
}

func (c *Client) saveTokenFor(clientID string, token *oauth2.Token, scopes []string) error {
	if c.tokenStore == nil {
		return errors.New("OAuth token store is not configured")
	}
	return c.tokenStore.Put(c.cacheKey(clientID), oauthcache.Record{
		Token:  *token,
		Scopes: append([]string(nil), scopes...),
	})
}

type persistingTokenSource struct {
	source   oauth2.TokenSource
	client   *Client
	clientID string
	scopes   []string
}

type cachedTokenSource struct {
	client   *Client
	config   oauth2.Config
	clientID string
	key      string
	scopes   []string
}

func (s *cachedTokenSource) Token() (*oauth2.Token, error) {
	lock, _ := cachedTokenLocks.LoadOrStore(s.key, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	record, err := s.client.tokenStore.Get(s.key)
	if err != nil {
		return nil, err
	}
	if record.Token.Valid() && (record.Token.Expiry.IsZero() || time.Until(record.Token.Expiry) > 30*time.Second) {
		return &record.Token, nil
	}

	token, err := s.client.oauthTokenSource(s.config, &record.Token).Token()
	if err != nil {
		token, err = (&authorizationCodeTokenSource{
			ctx:                 s.client.oauthContext(),
			config:              s.config,
			client:              s.client,
			resource:            s.client.resourceURL,
			authorizationPrompt: s.client.OAuth.AuthorizationPrompt,
		}).authorize()
		if err != nil {
			return nil, err
		}
	}
	if err := s.client.saveTokenFor(s.clientID, token, grantedScopes(token, s.scopes)); err != nil {
		return nil, fmt.Errorf("cache OAuth token: %w", err)
	}
	return token, nil
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	if err := s.client.saveTokenFor(s.clientID, token, grantedScopes(token, s.scopes)); err != nil {
		return nil, fmt.Errorf("cache OAuth token: %w", err)
	}
	return token, nil
}

func grantedScopes(token *oauth2.Token, requested []string) []string {
	if extra, ok := token.Extra("scope").(string); ok && extra != "" {
		return strings.Fields(extra)
	}
	return requested
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func selectIssuer(issuers []string, expected string) (string, error) {
	if len(issuers) == 0 {
		return "", errors.New("OAuth protected resource metadata has no authorization_servers")
	}
	if expected == "" {
		if err := urlpolicy.Validate(issuers[0], "OAuth authorization server issuer"); err != nil {
			return "", err
		}
		return issuers[0], nil
	}
	for _, issuer := range issuers {
		if issuer == expected {
			if err := urlpolicy.Validate(issuer, "OAuth authorization server issuer"); err != nil {
				return "", err
			}
			return issuer, nil
		}
	}
	return "", fmt.Errorf("discovery did not advertise expected issuer %q", expected)
}

func (c *Client) oauthContext() context.Context {
	return context.WithValue(c.ctx, oauth2.HTTPClient, c.HTTP)
}

func (c *Client) getJSON(endpoint string, destination any) error {
	request, err := http.NewRequestWithContext(c.ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("discovery returned %s", response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(destination)
}

type authorizationCodeTokenSource struct {
	ctx                 context.Context
	config              oauth2.Config
	client              *Client
	resource            string
	authorizationPrompt func(string)

	mu          sync.Mutex
	refresh     oauth2.TokenSource
	authorizing *authorizationResult
}

type authorizationCodeResult struct {
	token *oauth2.Token
	nonce string
}

type authenticatedUser struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	DisplayName       string `json:"display_name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Picture           string `json:"picture"`
}

// authorizeCode performs one authorization-code transaction. It uses a nonce
// only for the identity transaction, whose ID token is verified by the caller.
func (c *Client) authorizeCode(ctx context.Context, config oauth2.Config, resource string, useNonce bool) (authorizationCodeResult, error) {
	return runAuthorizationCode(ctx, c.HTTP, c.OAuth.AuthorizationPrompt, config, resource, useNonce)
}

func (c *Client) identity(token *oauth2.Token, nonce string) (authenticatedUser, error) {
	value, ok := token.Extra("id_token").(string)
	if !ok || value == "" {
		return authenticatedUser{}, errors.New("OAuth provider did not return an OIDC ID token")
	}
	if c.provider == nil {
		return authenticatedUser{}, errors.New("OIDC provider metadata is unavailable")
	}
	verifier := c.provider.Verifier(&oidc.Config{ClientID: c.activeClientID})
	idToken, err := verifier.Verify(c.ctx, value)
	if err != nil {
		return authenticatedUser{}, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	if idToken.Nonce != nonce {
		return authenticatedUser{}, errors.New("OIDC ID token nonce did not match")
	}
	if idToken.AccessTokenHash != "" {
		if err := idToken.VerifyAccessToken(token.AccessToken); err != nil {
			return authenticatedUser{}, fmt.Errorf("verify OIDC access-token hash: %w", err)
		}
	}
	var user authenticatedUser
	if err := idToken.Claims(&user); err != nil {
		return authenticatedUser{}, fmt.Errorf("read OIDC user claims: %w", err)
	}
	if c.provider != nil {
		if info, infoErr := c.provider.UserInfo(c.ctx, oauth2.StaticTokenSource(token)); infoErr == nil {
			var enriched authenticatedUser
			if claimsErr := info.Claims(&enriched); claimsErr == nil && enriched.Subject == user.Subject {
				if enriched.Name != "" {
					user.Name = enriched.Name
				}
				if enriched.DisplayName != "" {
					user.DisplayName = enriched.DisplayName
				}
				if enriched.PreferredUsername != "" {
					user.PreferredUsername = enriched.PreferredUsername
				}
				if enriched.Email != "" {
					user.Email = enriched.Email
				}
				if enriched.Picture != "" {
					user.Picture = enriched.Picture
				}
			}
		}
	}
	return user, nil
}

func (c *Client) selectScopes(ctx context.Context, user authenticatedUser, requested []string) ([]string, error) {
	selectionContext, cancel := context.WithTimeout(ctx, oauthCallbackTimeout)
	defer cancel()
	available := c.resourceScopes
	if len(available) == 0 {
		available = requested
	}
	if len(available) == 0 {
		return nil, errors.New("the protected resource did not advertise any selectable OAuth scopes")
	}
	selected := make(map[string]bool, len(requested))
	for _, scope := range requested {
		selected[scope] = true
	}
	listener, err := listenLoopback()
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	csrfToken, err := randomURLValue()
	if err != nil {
		return nil, fmt.Errorf("generate scope selector token: %w", err)
	}
	result := make(chan []string, 1)
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scope" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src https: http://127.0.0.1 http://localhost; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid scope selection", http.StatusBadRequest)
				return
			}
			providedToken := r.FormValue("csrf_token")
			if subtle.ConstantTimeCompare([]byte(providedToken), []byte(csrfToken)) != 1 {
				http.Error(w, "invalid scope selection token", http.StatusBadRequest)
				return
			}
			values := r.Form["scope"]
			allowed := make(map[string]bool, len(available))
			for _, scope := range available {
				allowed[scope] = true
			}
			chosen := make([]string, 0, len(values))
			for _, scope := range values {
				if allowed[scope] && !contains(chosen, scope) {
					chosen = append(chosen, scope)
				}
			}
			if len(chosen) == 0 {
				http.Error(w, "select at least one permission", http.StatusBadRequest)
				return
			}
			select {
			case result <- chosen:
			default:
				http.Error(w, "scope selection has already been submitted", http.StatusConflict)
				return
			}
			_, _ = io.WriteString(w, "Permissions selected. You can close this window.\n")
			return
		} else if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		providedToken := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(providedToken), []byte(csrfToken)) != 1 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, scopePage(user, available, selected, csrfToken))
	})}
	go server.Serve(listener)
	defer func() {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownContext)
	}()
	pageURL := "http://" + listener.Addr().String() + "/scope?token=" + url.QueryEscape(csrfToken)
	if c.OAuth.AuthorizationPrompt != nil {
		c.OAuth.AuthorizationPrompt(pageURL)
	}
	select {
	case selected := <-result:
		return selected, nil
	case <-selectionContext.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("scope selection timed out")
	}
}

func scopePage(user authenticatedUser, scopes []string, selected map[string]bool, csrfToken string) string {
	name := user.DisplayName
	if name == "" {
		name = user.Name
	}
	if name == "" {
		name = user.PreferredUsername
	}
	if name == "" {
		name = "authenticated user"
	}
	var b strings.Builder
	b.WriteString("<!doctype html><meta name=\"viewport\" content=\"width=device-width\"><title>pkgdepot permissions</title><style>body{font:16px system-ui;max-width:38rem;margin:3rem auto;padding:0 1rem}img{width:72px;height:72px;border-radius:50%;object-fit:cover;vertical-align:middle;margin-right:1rem}label{display:block;padding:.7rem;border:1px solid #ddd;border-radius:.5rem;margin:.5rem 0}button{padding:.7rem 1rem}</style>")
	if picture := safePictureURL(user.Picture); picture != "" {
		fmt.Fprintf(&b, "<img src=\"%s\" alt=\"\"><strong>", html.EscapeString(picture))
	} else {
		b.WriteString("<strong>")
	}
	fmt.Fprintf(&b, "Continue as %s?</strong>", html.EscapeString(name))
	if user.Email != "" {
		fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(user.Email))
	}
	fmt.Fprintf(&b, "<form method=post><input type=hidden name=csrf_token value=\"%s\"><h2>Select permissions</h2>", html.EscapeString(csrfToken))
	for _, scope := range scopes {
		checked := ""
		if selected[scope] {
			checked = " checked"
		}
		label := map[string]string{
			"package:publish": "Publish packages",
			"package:remove":  "Remove packages",
			"repo:create":     "Create repositories",
			"repo:remove":     "Remove repositories",
			"repo:rename":     "Rename repositories",
		}[scope]
		if label == "" {
			label = scope
		}
		fmt.Fprintf(&b, "<label><input type=checkbox name=scope value=\"%s\"%s> <strong>%s</strong><br><small>%s</small></label>", html.EscapeString(scope), checked, html.EscapeString(label), html.EscapeString(scope))
	}
	b.WriteString("<p><button>Continue</button></p></form>")
	return b.String()
}

func safePictureURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

type authorizationResult struct {
	done  chan struct{}
	token *oauth2.Token
	err   error
}

func (s *authorizationCodeTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	if s.refresh != nil {
		if token, err := s.refresh.Token(); err == nil {
			s.mu.Unlock()
			return token, nil
		}
		s.refresh = nil
	}
	if result := s.authorizing; result != nil {
		s.mu.Unlock()
		select {
		case <-result.done:
			return result.token, result.err
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
	result := &authorizationResult{done: make(chan struct{})}
	s.authorizing = result
	s.mu.Unlock()

	token, err := s.authorize()
	s.mu.Lock()
	if err == nil {
		s.refresh = s.config.TokenSource(context.WithValue(s.ctx, oauth2.HTTPClient, s.client.resourceHTTPClient()), token)
	}
	result.token = token
	result.err = err
	s.authorizing = nil
	close(result.done)
	s.mu.Unlock()
	return token, err
}

func listenLoopback() (net.Listener, error) {
	for _, endpoint := range []struct{ network, address string }{
		{network: "tcp4", address: "127.0.0.1:0"},
		{network: "tcp6", address: "[::1]:0"},
	} {
		listener, err := net.Listen(endpoint.network, endpoint.address)
		if err == nil {
			return listener, nil
		}
	}
	return nil, errors.New("unable to bind an IPv4 or IPv6 loopback OAuth callback listener")
}

func (s *authorizationCodeTokenSource) authorize() (*oauth2.Token, error) {
	httpClient := http.DefaultClient
	if s.client != nil && s.client.HTTP != nil {
		httpClient = s.client.HTTP
	}
	result, err := runAuthorizationCode(s.ctx, httpClient, s.authorizationPrompt, s.config, s.resource, false)
	if err != nil {
		return nil, err
	}
	return result.token, nil
}

const oauthCallbackTimeout = 5 * time.Minute

func runAuthorizationCode(ctx context.Context, httpClient *http.Client, prompt func(string), config oauth2.Config, resource string, useNonce bool) (authorizationCodeResult, error) {
	transactionContext, cancel := context.WithTimeout(ctx, oauthCallbackTimeout)
	defer cancel()
	state, err := randomURLValue()
	if err != nil {
		return authorizationCodeResult{}, fmt.Errorf("generate OAuth state: %w", err)
	}
	verifier := oauth2.GenerateVerifier()
	nonce := ""
	if useNonce {
		nonce, err = randomURLValue()
		if err != nil {
			return authorizationCodeResult{}, fmt.Errorf("generate OIDC nonce: %w", err)
		}
	}
	listener, err := listenLoopback()
	if err != nil {
		return authorizationCodeResult{}, err
	}
	defer listener.Close()
	redirectURL := fmt.Sprintf("http://%s/oauth/callback", listener.Addr().String())
	config.RedirectURL = redirectURL
	callback := make(chan authorizationCallback, 1)
	var callbackOnce sync.Once
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/oauth/callback" || request.Method != http.MethodGet {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			query := request.URL.Query()
			if query.Get("state") != state {
				http.Error(w, "OAuth callback state did not match", http.StatusBadRequest)
				return
			}
			result := authorizationCallback{code: query.Get("code")}
			if callbackError := query.Get("error"); callbackError != "" {
				result.err = oauthCallbackError(callbackError, query.Get("error_description"))
			} else if result.code == "" {
				result.err = errors.New("OAuth callback did not include an authorization code")
			}
			accepted := false
			callbackOnce.Do(func() {
				accepted = true
				callback <- result
			})
			if !accepted {
				http.Error(w, "OAuth callback has already been received", http.StatusConflict)
				return
			}
			if result.err != nil {
				http.Error(w, result.err.Error(), http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, "Authentication complete. You can close this window.\n")
		}),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownContext)
	}()
	authOptions := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if useNonce {
		authOptions = append(authOptions, oidc.Nonce(nonce))
	}
	if resource != "" {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("resource", resource))
	}
	if prompt != nil {
		prompt(config.AuthCodeURL(state, authOptions...))
	}
	select {
	case result := <-callback:
		if result.err != nil {
			return authorizationCodeResult{}, result.err
		}
		exchangeOptions := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
		if resource != "" {
			exchangeOptions = append(exchangeOptions, oauth2.SetAuthURLParam("resource", resource))
		}
		exchangeContext := context.WithValue(transactionContext, oauth2.HTTPClient, httpClient)
		token, err := config.Exchange(exchangeContext, result.code, exchangeOptions...)
		if err != nil {
			return authorizationCodeResult{}, fmt.Errorf("exchange OAuth authorization code: %w", err)
		}
		return authorizationCodeResult{token: token, nonce: nonce}, nil
	case <-transactionContext.Done():
		if ctx.Err() != nil {
			return authorizationCodeResult{}, ctx.Err()
		}
		return authorizationCodeResult{}, errors.New("OAuth callback timed out")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return authorizationCodeResult{}, fmt.Errorf("serve OAuth callback: %w", err)
		}
		return authorizationCodeResult{}, errors.New("OAuth callback server stopped unexpectedly")
	}
}

func oauthCallbackError(code, description string) error {
	code = sanitizeOAuthText(code, 128)
	description = sanitizeOAuthText(description, 512)
	if description == "" {
		return fmt.Errorf("OAuth authorization failed: %s", code)
	}
	return fmt.Errorf("OAuth authorization failed: %s: %s", code, description)
}

func sanitizeOAuthText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

type authorizationCallback struct {
	code string
	err  error
}

func randomURLValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func normalizeResourceIdentifier(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return strings.TrimRight(value, "/")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return parsed.String()
}

func wellKnownURL(issuer, document string) string {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Path == "" || parsed.Path == "/" {
		return strings.TrimRight(issuer, "/") + "/.well-known/" + document
	}
	path := strings.TrimRight(parsed.Path, "/")
	parsed.Path = "/.well-known/" + document + path
	parsed.RawPath = ""
	return parsed.String()
}

func (c *Client) do(client *http.Client, request *http.Request, destination any) error {
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError api.ErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil || apiError.Error == "" {
			return fmt.Errorf("server returned %s", response.Status)
		}
		return &APIError{Status: response.Status, Code: apiError.Code, Message: apiError.Error}
	}
	if destination == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func addFile(writer *multipart.Writer, field, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open %s: %w", field, err)
	}
	defer file.Close()
	part, err := writer.CreateFormFile(field, path.Base(filename))
	if err != nil {
		return fmt.Errorf("create %s form field: %w", field, err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("write %s form field: %w", field, err)
	}
	return nil
}
