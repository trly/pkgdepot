package auth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/trly/pkgdepot/internal/auth"
)

func TestOIDCValidatorDiscoversAndValidatesAccessTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                issuer.URL,
				"jwks_uri":                              issuer.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
				"scopes_supported":                      []string{auth.ScopePublish, auth.ScopeRemove},
			})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "key", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer issuer.Close()

	validator, err := auth.NewOIDCValidator(context.Background(), auth.OIDCOptions{Issuer: issuer.URL, Audience: "https://packages.example", Algorithms: []string{"RS256"}})
	if err != nil {
		t.Fatal(err)
	}
	valid := signedToken(t, key, jwt.MapClaims{
		"iss":       issuer.URL,
		"aud":       "https://packages.example",
		"exp":       time.Now().Add(time.Minute).Unix(),
		"iat":       time.Now().Add(-time.Minute).Unix(),
		"sub":       "user-1",
		"client_id": "client-1",
		"jti":       "token-1",
		"scp":       []string{auth.ScopePublish, auth.ScopeRemove},
	}, "at+jwt")
	claims, err := validator.Validate(context.Background(), valid)
	if err != nil || !auth.HasScope(claims, auth.ScopePublish) || !auth.HasScope(claims, auth.ScopeRemove) {
		t.Fatalf("Validate = %#v, %v", claims, err)
	}
	standardScope := jwt.MapClaims{
		"iss":       issuer.URL,
		"aud":       "https://packages.example",
		"exp":       time.Now().Add(time.Minute).Unix(),
		"scope":     auth.ScopePublish + " " + auth.ScopeRemove,
		"sub":       "user-1",
		"client_id": "client-1",
		"jti":       "token-2",
		"iat":       time.Now().Add(-time.Minute).Unix(),
	}
	claims, err = validator.Validate(context.Background(), signedToken(t, key, standardScope, "at+jwt"))
	if err != nil || !auth.HasScope(claims, auth.ScopePublish) || !auth.HasScope(claims, auth.ScopeRemove) {
		t.Fatalf("standard scope Validate = %#v, %v", claims, err)
	}
	for name, claims := range map[string]jwt.MapClaims{
		"wrong issuer":   {"iss": issuer.URL + "/other", "aud": "https://packages.example", "exp": time.Now().Add(time.Minute).Unix()},
		"wrong audience": {"iss": issuer.URL, "aud": "other", "exp": time.Now().Add(time.Minute).Unix()},
		"expired":        {"iss": issuer.URL, "aud": "https://packages.example", "exp": time.Now().Add(-time.Minute).Unix()},
	} {
		t.Run(name, func(t *testing.T) {
			addAccessTokenClaims(claims)
			if _, err := validator.Validate(context.Background(), signedToken(t, key, claims, "at+jwt")); err == nil {
				t.Fatal("accepted invalid access token")
			}
		})
	}

	for name, tokenType := range map[string]string{
		"id token type":    "JWT",
		"missing type":     "",
		"application type": "application/at+jwt",
		"mixed-case type":  "at+JWT",
	} {
		t.Run(name, func(t *testing.T) {
			claims := validAccessTokenClaims(issuer.URL)
			if tokenType == "" {
				if _, err := validator.Validate(context.Background(), signedToken(t, key, claims)); err == nil {
					t.Fatal("accepted token without access-token typ")
				}
				return
			}
			if strings.EqualFold(tokenType, "at+jwt") || strings.EqualFold(tokenType, "application/at+jwt") {
				if _, err := validator.Validate(context.Background(), signedToken(t, key, claims, tokenType)); err != nil {
					t.Fatalf("rejected RFC 9068 media type: %v", err)
				}
				return
			}
			if _, err := validator.Validate(context.Background(), signedToken(t, key, claims, tokenType)); err == nil {
				t.Fatal("accepted non-access-token typ")
			}
		})
	}

	for name, mutate := range map[string]func(jwt.MapClaims){
		"missing subject":   func(c jwt.MapClaims) { delete(c, "sub") },
		"missing client id": func(c jwt.MapClaims) { delete(c, "client_id") },
		"missing issued at": func(c jwt.MapClaims) { delete(c, "iat") },
		"missing jwt id":    func(c jwt.MapClaims) { delete(c, "jti") },
		"invalid issued at": func(c jwt.MapClaims) { c["iat"] = "now" },
		"malformed scp":     func(c jwt.MapClaims) { c["scp"] = auth.ScopeRemove },
	} {
		t.Run(name, func(t *testing.T) {
			claims := validAccessTokenClaims(issuer.URL)
			mutate(claims)
			if _, err := validator.Validate(context.Background(), signedToken(t, key, claims, "at+jwt")); err == nil {
				t.Fatal("accepted token with invalid RFC 9068 claims")
			}
		})
	}

}

func TestOIDCValidatorRejectsIncompatibleProviderMetadata(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"missing JWKS URI": func(metadata map[string]any) {
			delete(metadata, "jwks_uri")
		},
	} {
		t.Run(name, func(t *testing.T) {
			var issuer *httptest.Server
			issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				metadata := map[string]any{
					"issuer":                                issuer.URL,
					"jwks_uri":                              issuer.URL + "/keys",
					"id_token_signing_alg_values_supported": []string{"RS256"},
					"scopes_supported":                      []string{auth.ScopePublish, auth.ScopeRemove},
				}
				mutate(metadata)
				_ = json.NewEncoder(w).Encode(metadata)
			}))
			defer issuer.Close()

			_, err := auth.NewOIDCValidator(context.Background(), auth.OIDCOptions{Issuer: issuer.URL, Audience: "https://packages.example", Algorithms: []string{"RS256"}})
			if err == nil {
				t.Fatal("accepted incompatible provider metadata")
			}
		})
	}
}

func TestOIDCValidatorDefaultAlgorithmAcceptsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":   issuer.URL,
				"jwks_uri": issuer.URL + "/keys",
			})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "key", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer issuer.Close()

	validator, err := auth.NewOIDCValidator(context.Background(), auth.OIDCOptions{Issuer: issuer.URL, Audience: "https://packages.example"})
	if err != nil {
		t.Fatal(err)
	}
	claims := validAccessTokenClaims(issuer.URL)
	if _, err := validator.Validate(context.Background(), signedToken(t, key, claims, "at+jwt")); err != nil {
		t.Fatalf("rejected RS256 token with default algorithms: %v", err)
	}
}

func TestOIDCValidatorDefaultAlgorithmRejectsES256(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   issuer.URL,
			"jwks_uri": issuer.URL + "/keys",
		})
	}))
	defer issuer.Close()

	validator, err := auth.NewOIDCValidator(context.Background(), auth.OIDCOptions{Issuer: issuer.URL, Audience: "https://packages.example"})
	if err != nil {
		t.Fatal(err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, validAccessTokenClaims(issuer.URL))
	token.Header["kid"] = "key"
	token.Header["typ"] = "at+jwt"
	value, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validator.Validate(context.Background(), value); err == nil {
		t.Fatal("accepted ES256 token with default RS256 algorithm")
	}
}

func validAccessTokenClaims(issuer string) jwt.MapClaims {
	claims := jwt.MapClaims{"iss": issuer, "aud": "https://packages.example", "exp": time.Now().Add(time.Minute).Unix(), "scp": []string{auth.ScopePublish}}
	addAccessTokenClaims(claims)
	return claims
}

func addAccessTokenClaims(claims jwt.MapClaims) {
	claims["iat"] = time.Now().Add(-time.Minute).Unix()
	claims["sub"] = "user-1"
	claims["client_id"] = "client-1"
	claims["jti"] = "token-1"
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims, tokenType ...string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "key"
	if len(tokenType) > 0 {
		token.Header["typ"] = tokenType[0]
	}
	value, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
