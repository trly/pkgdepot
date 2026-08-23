package cimd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/trly/pkgdepot/internal/cimd"
)

func TestNewMetadata(t *testing.T) {
	metadata, err := cimd.NewMetadata("https://packages.example/pkgdepot", "pkgdepot CLI")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ClientID != "https://packages.example/pkgdepot/oauth/client-metadata.json" {
		t.Fatalf("client_id = %q", metadata.ClientID)
	}
	if metadata.TokenEndpointAuthMethod != "none" || metadata.ApplicationType != "native" {
		t.Fatalf("metadata = %+v", metadata)
	}
	wantRedirects := []string{
		"http://127.0.0.1/oauth/callback",
		"http://[::1]/oauth/callback",
	}
	if !reflect.DeepEqual(metadata.RedirectURIs, wantRedirects) {
		t.Fatalf("redirect_uris = %v, want %v", metadata.RedirectURIs, wantRedirects)
	}
}

func TestProfileMetadataUsesDistinctClientIDs(t *testing.T) {
	publisher, err := cimd.NewProfileMetadata("https://packages.example", cimd.PublisherMetadataPath, "pkgdepot CLI - Publisher")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := cimd.NewProfileMetadata("https://packages.example", cimd.AdminMetadataPath, "pkgdepot CLI - Admin")
	if err != nil {
		t.Fatal(err)
	}
	if publisher.ClientID == admin.ClientID || publisher.ClientID != "https://packages.example/oauth/clients/cli-publisher" || admin.ClientID != "https://packages.example/oauth/clients/cli-admin" {
		t.Fatalf("profile client IDs = %q, %q", publisher.ClientID, admin.ClientID)
	}
	if publisher.ClientName != "pkgdepot CLI - Publisher" || admin.ClientName != "pkgdepot CLI - Admin" {
		t.Fatalf("profile names = %q, %q", publisher.ClientName, admin.ClientName)
	}
}

func TestMetadataURLPreservesEscapedPath(t *testing.T) {
	url, err := cimd.MetadataURL("https://packages.example/tenant%2Fname")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://packages.example/tenant%2Fname/oauth/client-metadata.json" {
		t.Fatalf("client_id = %q", url)
	}
}

func TestMetadataURLRejectsNonHTTPS(t *testing.T) {
	if _, err := cimd.MetadataURL("http://packages.example"); err == nil {
		t.Fatal("accepted non-HTTPS CIMD URL")
	}
}

func TestMetadataURLRejectsDotPathComponents(t *testing.T) {
	for _, resource := range []string{
		"https://packages.example/.",
		"https://packages.example/..",
		"https://packages.example/a/./b",
		"https://packages.example/a/../b",
		"https://packages.example/%2e/b",
		"https://packages.example/%2E%2E/b",
	} {
		if _, err := cimd.MetadataURL(resource); err == nil {
			t.Errorf("MetadataURL(%q) = nil error, want error", resource)
		}
	}
}

func TestMetadataURLAcceptsDottedPathNames(t *testing.T) {
	for _, resource := range []string{
		"https://packages.example/.well-known",
		"https://packages.example/a..b",
		"https://packages.example/a%2Fb",
	} {
		if _, err := cimd.MetadataURL(resource); err != nil {
			t.Errorf("MetadataURL(%q) = %v, want nil", resource, err)
		}
	}
}

func TestHandler(t *testing.T) {
	recorder := httptest.NewRecorder()
	cimd.Handler("https://packages.example", "pkgdepot CLI").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, cimd.MetadataPath, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var metadata cimd.Metadata
	if err := json.NewDecoder(recorder.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ClientID != "https://packages.example/oauth/client-metadata.json" {
		t.Fatalf("client_id = %q", metadata.ClientID)
	}
}
