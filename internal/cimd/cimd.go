package cimd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	MetadataPath          = "/oauth/client-metadata.json"
	PublisherMetadataPath = "/oauth/clients/cli-publisher"
	AdminMetadataPath     = "/oauth/clients/cli-admin"
)

// Metadata is an OAuth Client ID Metadata Document for the pkgdepot CLI.
type Metadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ApplicationType         string   `json:"application_type"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	RedirectURIs            []string `json:"redirect_uris"`
}

func MetadataURL(resource string) (string, error) {
	return MetadataURLForPath(resource, MetadataPath)
}

func MetadataURLForPath(resource, metadataPath string) (string, error) {
	if metadataPath == "" || !strings.HasPrefix(metadataPath, "/") {
		return "", errors.New("CIMD metadata path must be absolute")
	}
	parsed, err := url.Parse(resource)
	if err != nil {
		return "", fmt.Errorf("parse resource URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("CIMD requires an HTTPS resource URL without userinfo, query, or fragment")
	}
	for _, component := range strings.Split(parsed.EscapedPath(), "/") {
		decoded, err := url.PathUnescape(component)
		if err != nil {
			return "", fmt.Errorf("validate resource URL path: %w", err)
		}
		if decoded == "." || decoded == ".." {
			return "", errors.New("CIMD client ID URL must not contain single-dot or double-dot path components")
		}
	}
	rawPath := parsed.RawPath
	parsed.Path = strings.TrimRight(parsed.Path, "/") + metadataPath
	if rawPath != "" {
		parsed.RawPath = strings.TrimRight(rawPath, "/") + metadataPath
	}
	return parsed.String(), nil
}

func RedirectURLs() []string {
	return []string{
		"http://127.0.0.1/oauth/callback",
		"http://[::1]/oauth/callback",
	}
}

func NewMetadata(resource, name string) (Metadata, error) {
	return NewProfileMetadata(resource, MetadataPath, name)
}

func NewProfileMetadata(resource, metadataPath, name string) (Metadata, error) {
	clientID, err := MetadataURLForPath(resource, metadataPath)
	if err != nil {
		return Metadata{}, err
	}
	if name == "" {
		name = "pkgdepot CLI"
	}
	return Metadata{
		ClientID:                clientID,
		ClientName:              name,
		ApplicationType:         "native",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		RedirectURIs:            RedirectURLs(),
	}, nil
}

func Handler(resource, name string) http.Handler {
	return ProfileHandler(resource, MetadataPath, name)
}

func ProfileHandler(resource, metadataPath, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metadata, err := NewProfileMetadata(resource, metadataPath, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metadata)
	})
}
