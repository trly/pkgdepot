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
	ScopePublish = "package:publish"
	ScopeRemove  = "package:remove"
)

// Claims are the access-token claims relevant to a protected resource server.
type Claims struct {
	Scopes []string
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
