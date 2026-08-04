package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/trly/pkgdepot/internal/httpclient"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPublishReportsEarlyServerResponse(t *testing.T) {
	packagePath := t.TempDir() + "/example.pkg.tar.zst"
	if err := writeTestPackage(packagePath); err != nil {
		t.Fatal(err)
	}

	client := httpclient.New("http://pkgdepot.test", "secret")
	client.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		request.Body.Close()
		return &http.Response{
			Status:     "404 Not Found",
			StatusCode: http.StatusNotFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"route not found","code":"not_found"}`)),
			Request:    request,
		}, nil
	})}

	_, err := client.Publish(context.Background(), "stable", "x86_64", packagePath, "")
	if err == nil || err.Error() != "server returned 404 Not Found: route not found" {
		t.Fatalf("Publish() error = %v", err)
	}
	var apiError *httpclient.APIError
	if !errors.As(err, &apiError) || apiError.Code != "not_found" {
		t.Fatalf("Publish() error code = %#v, want not_found", apiError)
	}
}

func writeTestPackage(filename string) error {
	return os.WriteFile(filename, make([]byte, 1<<20), 0o600)
}
