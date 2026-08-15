package cimd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const MetadataPath = "/oauth/client-metadata.json"

var LoopbackPorts = []int{8085, 8086, 8087, 8088, 8089}

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
	parsed.Path = strings.TrimRight(parsed.Path, "/") + MetadataPath
	if rawPath != "" {
		parsed.RawPath = strings.TrimRight(rawPath, "/") + MetadataPath
	}
	return parsed.String(), nil
}

func RedirectURLs() []string {
	urls := make([]string, len(LoopbackPorts))
	for i, port := range LoopbackPorts {
		urls[i] = fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", port)
	}
	return urls
}

func NewMetadata(resource, name string) (Metadata, error) {
	clientID, err := MetadataURL(resource)
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
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		metadata, err := NewMetadata(resource, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metadata)
	})
}
