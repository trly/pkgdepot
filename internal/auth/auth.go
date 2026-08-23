// Package auth contains resource-server authentication contracts.
package auth

import (
	"context"
	"errors"
	"log"
	"slices"
	"strings"
	"time"
)

var (
	ErrMissingCredentials = errors.New("missing bearer credentials")
	ErrInvalidRequest     = errors.New("invalid bearer request")
	ErrInvalidToken       = errors.New("invalid bearer token")
)

const (
	ScopePublish          = "package:publish"
	ScopeRemove           = "package:remove"
	ScopeRepositoryCreate = "repo:create"
	ScopeRepositoryRemove = "repo:remove"
	ScopeRepositoryRename = "repo:rename"
	DefaultRoleClaim      = "pkgdepot_roles"
)

// Claims are the access-token claims relevant to a protected resource server.
type Claims struct {
	Scopes   []string
	Roles    []string
	Subject  string
	ClientID string
}

// DefaultRoleScopes returns the built-in roles for a new PKGdepot server.
func DefaultRoleScopes() map[string][]string {
	return map[string][]string{
		"admin":     {ScopePublish, ScopeRemove, ScopeRepositoryCreate, ScopeRepositoryRemove, ScopeRepositoryRename},
		"publisher": {ScopePublish},
	}
}

type Validator interface {
	Validate(context.Context, string) (Claims, error)
}

// OIDCOptions configures an OIDC-discovered signed JWT access-token validator.
type OIDCOptions struct {
	Issuer           string
	Audience         string
	Algorithms       []string
	KeyCacheLifetime time.Duration
	RoleClaim        string
}

// NewOIDCValidator creates an OIDC-discovered signed JWT access-token validator.
func NewOIDCValidator(ctx context.Context, options OIDCOptions) (OIDCValidator, error) {
	verifier, err := newOIDCVerifier(ctx, options)
	if err != nil {
		return OIDCValidator{}, err
	}
	return OIDCValidator{verifier: verifier}, nil
}

type OIDCValidator struct {
	verifier oidcVerifier
}

func (v OIDCValidator) Validate(ctx context.Context, value string) (Claims, error) {
	claims, err := v.verifier.Verify(ctx, value)
	if err != nil {
		log.Printf("token verification failed: %v", err)
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func HasScope(claims Claims, scope string) bool {
	return slices.Contains(claims.Scopes, scope)
}

// AuthorizeRoles reports whether the claims grant the requested scope. User
// tokens must have both the OAuth scope and a role that permits it. Role-less
// client-credentials tokens may authorize from their OAuth scopes when their
// subject matches subjectTemplate.
func AuthorizeRoles(claims Claims, scope string, roleScopes map[string][]string, subjectTemplate string) bool {
	if len(claims.Roles) == 0 {
		if subjectTemplate == "" || !MatchClientCredentialsSubject(claims, subjectTemplate) {
			return false
		}
		return HasScope(claims, scope)
	}
	if !HasScope(claims, scope) {
		return false
	}
	for _, role := range claims.Roles {
		if slices.Contains(roleScopes[role], scope) {
			return true
		}
	}
	return false
}

// MatchClientCredentialsSubject reports whether the token subject matches the
// configured client-credentials template. The template must contain exactly one
// {client_id} placeholder, which is expanded to the signed client_id claim.
func MatchClientCredentialsSubject(claims Claims, subjectTemplate string) bool {
	if claims.Subject == "" || subjectTemplate == "" {
		return false
	}
	return claims.Subject == strings.ReplaceAll(subjectTemplate, "{client_id}", claims.ClientID)
}

func BearerToken(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", ErrMissingCredentials
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrInvalidRequest
	}
	return parts[1], nil
}
