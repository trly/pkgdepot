// Package api contains the stable JSON contract exposed by pkgdepot's HTTP API.
//
// These types deliberately do not expose the repository implementation or the
// Arch package parser. Internal representations can evolve without changing
// the API contract.
package api

type Package struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Architecture string   `json:"architecture"`
	Description  string   `json:"description,omitempty"`
	Filename     string   `json:"filename,omitempty"`
	Size         int64    `json:"size,omitempty"`
	Depends      []string `json:"depends,omitempty"`
}

type Repository struct {
	Name          string   `json:"name"`
	Architectures []string `json:"architectures"`
}

type RenameRepositoryRequest struct {
	Name string `json:"name"`
}

// ErrorResponse is returned by API endpoints for non-success responses.
// Error is retained as the human-readable field for compatibility; clients
// should use Code for branching and display Error to users.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}
