package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trly/pkgdepot/internal/httpapi"
	"github.com/trly/pkgdepot/internal/repository"
)

type commands struct{}

func (commands) Add(context.Context, string, string) error    { return nil }
func (commands) Remove(context.Context, string, string) error { return nil }

func TestHealthAndAuthentication(t *testing.T) {
	service := repository.New(t.TempDir(), commands{})
	if err := service.Initialize(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(service, "secret"))
	defer server.Close()

	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/repositories/test/x86_64/packages", nil)
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

	request.Header.Set("Authorization", "secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("malformed authorization status = %d", response.StatusCode)
	}

	request.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d", response.StatusCode)
	}
}
