package httpclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestAuthorizationCodeTokenSourceDoesNotHoldMutexWhileAuthorizing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	source := &authorizationCodeTokenSource{
		ctx: ctx,
		config: oauth2.Config{
			ClientID: "client",
			Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example/authorize"},
		},
		authorizationPrompt: func(string) { close(started) },
	}

	result := make(chan error, 1)
	go func() {
		_, err := source.Token()
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("authorization did not start")
	}
	if !source.mu.TryLock() {
		t.Fatal("authorization held the token source mutex while awaiting the callback")
	}
	source.mu.Unlock()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Token() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Token() did not stop after cancellation")
	}
}

func TestOAuthCallbackErrorIsBoundedAndSanitized(t *testing.T) {
	err := oauthCallbackError("access_denied", "line one\n"+string(make([]byte, 600)))
	if len(err.Error()) > 600 {
		t.Fatalf("callback error length = %d, want bounded", len(err.Error()))
	}
	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Fatalf("callback error contains control characters: %q", err)
	}
}
