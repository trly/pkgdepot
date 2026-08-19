package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jwt "github.com/golang-jwt/jwt/v5"
)

func TestExpiringKeySetRefreshesAfterLifetime(t *testing.T) {
	now := time.Unix(100, 0)
	created := 0
	set := &expiringKeySet{
		ctx:      context.Background(),
		jwksURL:  "https://issuer.example/keys",
		lifetime: time.Minute,
		now:      func() time.Time { return now },
		newKeySet: func(context.Context, string) oidc.KeySet {
			created++
			return acceptingKeySet{}
		},
	}

	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("created %d key sets before expiry, want 1", created)
	}

	now = now.Add(time.Minute)
	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("created %d key sets after expiry, want 2", created)
	}
}

func TestExpiringKeySetRefreshesOnceConcurrently(t *testing.T) {
	var mu sync.Mutex
	created := 0
	set := &expiringKeySet{
		ctx:      context.Background(),
		jwksURL:  "https://issuer.example/keys",
		lifetime: time.Minute,
		now:      time.Now,
		newKeySet: func(context.Context, string) oidc.KeySet {
			mu.Lock()
			defer mu.Unlock()
			created++
			return acceptingKeySet{}
		},
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = set.VerifySignature(context.Background(), "token")
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if created != 1 {
		t.Fatalf("created %d key sets concurrently, want 1", created)
	}
}

func TestExpiringKeySetRejectsTokenAfterKeyRefresh(t *testing.T) {
	now := time.Unix(100, 0)
	created := 0
	set := &expiringKeySet{
		ctx:      context.Background(),
		jwksURL:  "https://issuer.example/keys",
		lifetime: time.Minute,
		now:      func() time.Time { return now },
		newKeySet: func(context.Context, string) oidc.KeySet {
			created++
			return fakeKeySet{value: created}
		},
	}

	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := set.VerifySignature(context.Background(), "token"); err == nil {
		t.Fatal("accepted token rejected by refreshed key set")
	}
}

func TestExpiringKeySetReusesKeySetDuringActiveTraffic(t *testing.T) {
	now := time.Unix(100, 0)
	created := 0
	set := &expiringKeySet{
		ctx:      context.Background(),
		jwksURL:  "https://issuer.example/keys",
		lifetime: time.Minute,
		now:      func() time.Time { return now },
		newKeySet: func(context.Context, string) oidc.KeySet {
			created++
			return acceptingKeySet{}
		},
	}

	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("created %d key sets, want 1", created)
	}

	for i := 0; i < 10; i++ {
		now = now.Add(5 * time.Second)
		if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d key sets during active traffic, want 1", created)
	}

	now = now.Add(10 * time.Second)
	if _, err := set.VerifySignature(context.Background(), "token"); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("created %d key sets after lifetime expiry, want 2", created)
	}
}

func TestExpiringKeySetRejectsRemovedHTTPJWKSKeyAfterLifetime(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	keys := []map[string]string{rsaJWK(oldKey, "old")}
	requests := 0
	requestCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return requests
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/keys" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		requests++
		if err := json.NewEncoder(w).Encode(map[string]any{"keys": keys}); err != nil {
			t.Errorf("write JWKS: %v", err)
		}
	}))
	defer server.Close()

	now := time.Unix(100, 0)
	set := newExpiringKeySet(context.Background(), server.URL+"/keys", time.Minute)
	set.now = func() time.Time { return now }
	oldToken := signedKeySetToken(t, oldKey, "old")
	if _, err := set.VerifySignature(context.Background(), oldToken); err != nil {
		t.Fatalf("verify token signed with initial key: %v", err)
	}
	if got := requestCount(); got != 1 {
		t.Fatalf("JWKS requests = %d, want 1", got)
	}

	mu.Lock()
	keys = []map[string]string{rsaJWK(newKey, "new")}
	mu.Unlock()
	for range 10 {
		now = now.Add(5 * time.Second)
		if _, err := set.VerifySignature(context.Background(), oldToken); err != nil {
			t.Fatalf("verify cached token before key set expiry: %v", err)
		}
	}
	if got := requestCount(); got != 1 {
		t.Fatalf("JWKS requests before expiry = %d, want 1", got)
	}

	now = now.Add(10 * time.Second)
	if _, err := set.VerifySignature(context.Background(), oldToken); err == nil {
		t.Fatal("accepted token signed by removed JWKS key")
	}
	if got := requestCount(); got != 2 {
		t.Fatalf("JWKS requests after expiry = %d, want 2", got)
	}

	newToken := signedKeySetToken(t, newKey, "new")
	if _, err := set.VerifySignature(context.Background(), newToken); err != nil {
		t.Fatalf("verify token signed with rotated key: %v", err)
	}
	if got := requestCount(); got != 2 {
		t.Fatalf("JWKS requests after rotated-key verification = %d, want 2", got)
	}
}

func rsaJWK(key *rsa.PrivateKey, kid string) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func signedKeySetToken(t *testing.T, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "test"})
	token.Header["kid"] = kid
	value, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type fakeKeySet struct {
	value int
}

func (s fakeKeySet) VerifySignature(_ context.Context, token string) ([]byte, error) {
	if s.value == 1 && token == "token" {
		return []byte(token), nil
	}
	return nil, errors.New("signature rejected")
}

type acceptingKeySet struct{}

func (acceptingKeySet) VerifySignature(_ context.Context, token string) ([]byte, error) {
	return []byte(token), nil
}
