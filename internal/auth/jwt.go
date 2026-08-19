package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/trly/pkgdepot/internal/urlpolicy"
)

type oidcVerifier interface {
	Verify(context.Context, string) (Claims, error)
}

type discoveredVerifier struct {
	verifier  *oidc.IDTokenVerifier
	roleClaim string
}

const defaultKeyCacheLifetime = 15 * time.Minute

// expiringKeySet bounds how long a previously fetched signing key remains
// trusted. The key set is replaced after lifetime has elapsed since it was
// created, regardless of verification activity. This catches keys removed by
// the provider.
type expiringKeySet struct {
	mu        sync.Mutex
	ctx       context.Context
	jwksURL   string
	lifetime  time.Duration
	now       func() time.Time
	newKeySet func(context.Context, string) oidc.KeySet
	keySet    oidc.KeySet
	createdAt time.Time
}

func newExpiringKeySet(ctx context.Context, jwksURL string, lifetime time.Duration) *expiringKeySet {
	if lifetime <= 0 {
		lifetime = defaultKeyCacheLifetime
	}
	return &expiringKeySet{ctx: ctx, jwksURL: jwksURL, lifetime: lifetime, now: time.Now, newKeySet: func(ctx context.Context, url string) oidc.KeySet {
		return oidc.NewRemoteKeySet(ctx, url)
	}}
}

func (s *expiringKeySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	s.mu.Lock()
	now := s.now()
	if s.keySet == nil || now.Sub(s.createdAt) >= s.lifetime {
		s.keySet = s.newKeySet(s.ctx, s.jwksURL)
		s.createdAt = now
	}
	keySet := s.keySet
	s.mu.Unlock()
	return keySet.VerifySignature(ctx, jwt)
}

func newOIDCVerifier(ctx context.Context, options OIDCOptions) (*discoveredVerifier, error) {
	if strings.TrimSpace(options.Issuer) == "" {
		return nil, fmt.Errorf("authentication issuer is required")
	}
	if strings.TrimSpace(options.Audience) == "" {
		return nil, fmt.Errorf("authentication audience is required")
	}
	if err := urlpolicy.Validate(options.Issuer, "authentication issuer"); err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, options.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	var metadata struct {
		Issuer   string `json:"issuer"`
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
		JWKSURL  string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read OIDC provider metadata: %w", err)
	}
	if err := urlpolicy.Validate(metadata.Issuer, "issuer"); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{"authorization endpoint": metadata.AuthURL, "token endpoint": metadata.TokenURL, "JWKS URI": metadata.JWKSURL} {
		if value != "" {
			if err := urlpolicy.ValidateEndpoint(value, name); err != nil {
				return nil, err
			}
		}
	}
	if metadata.JWKSURL == "" {
		return nil, fmt.Errorf("OIDC provider metadata has no JWKS URI")
	}
	algorithms := options.Algorithms
	if len(algorithms) == 0 {
		algorithms = []string{"RS256"}
	}
	roleClaim := options.RoleClaim
	if roleClaim == "" {
		roleClaim = DefaultRoleClaim
	}
	keySet := newExpiringKeySet(ctx, metadata.JWKSURL, options.KeyCacheLifetime)
	return &discoveredVerifier{verifier: oidc.NewVerifier(options.Issuer, keySet, &oidc.Config{
		ClientID:             options.Audience,
		SupportedSigningAlgs: algorithms,
	}), roleClaim: roleClaim}, nil
}

func (v *discoveredVerifier) Verify(ctx context.Context, value string) (Claims, error) {
	if err := validateAccessTokenHeader(value); err != nil {
		return Claims{}, err
	}
	token, err := v.verifier.Verify(ctx, value)
	if err != nil {
		return Claims{}, err
	}
	var raw struct {
		Scopes   []string    `json:"scp"`
		Scope    string      `json:"scope"`
		Subject  string      `json:"sub"`
		ClientID string      `json:"client_id"`
		IssuedAt json.Number `json:"iat"`
		JWTID    string      `json:"jti"`
	}
	if err := token.Claims(&raw); err != nil {
		return Claims{}, err
	}
	if raw.Subject == "" || raw.ClientID == "" || raw.JWTID == "" {
		return Claims{}, fmt.Errorf("access token is missing a required claim")
	}
	if issuedAt, err := raw.IssuedAt.Int64(); err != nil || issuedAt <= 0 {
		return Claims{}, fmt.Errorf("access token has an invalid iat claim")
	}
	var allClaims map[string]json.RawMessage
	if err := token.Claims(&allClaims); err != nil {
		return Claims{}, err
	}
	roles, err := stringSliceClaim(allClaims[v.roleClaim])
	if err != nil {
		return Claims{}, fmt.Errorf("access token has an invalid %q claim: %w", v.roleClaim, err)
	}
	return Claims{Scopes: append(raw.Scopes, strings.Fields(raw.Scope)...), Roles: roles, Subject: raw.Subject, ClientID: raw.ClientID}, nil
}

func stringSliceClaim(value json.RawMessage) ([]string, error) {
	if len(value) == 0 || string(value) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(value, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func validateAccessTokenHeader(value string) error {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return fmt.Errorf("access token is not a compact JWT")
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode access token header: %w", err)
	}
	var raw struct {
		Type string `json:"typ"`
	}
	if err := json.Unmarshal(header, &raw); err != nil {
		return fmt.Errorf("decode access token header: %w", err)
	}
	if raw.Type == "" {
		return fmt.Errorf("access token is missing the required typ header (expected %q per RFC 9068)", "at+jwt")
	}
	if !strings.EqualFold(raw.Type, "at+jwt") && !strings.EqualFold(raw.Type, "application/at+jwt") {
		return fmt.Errorf("access token has an unsupported typ header %q (expected %q or %q per RFC 9068)", raw.Type, "at+jwt", "application/at+jwt")
	}
	return nil
}
