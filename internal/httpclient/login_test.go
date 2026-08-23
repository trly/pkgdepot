package httpclient

import (
	"testing"

	"github.com/trly/pkgdepot/internal/auth"
)

func TestLoginScopesDefaults(t *testing.T) {
	all := []string{auth.ScopePublish, auth.ScopeRemove, auth.ScopeRepositoryCreate}
	if got, err := loginScopes(AccessPublisher, nil, all); err != nil || len(got) != 1 || got[0] != auth.ScopePublish {
		t.Fatalf("publisher defaults = %v, %v", got, err)
	}
	if got, err := loginScopes(AccessAdmin, nil, all); err != nil || len(got) != len(all) {
		t.Fatalf("admin defaults = %v, %v", got, err)
	}
}

func TestLoginScopesValidatesProfileAndResource(t *testing.T) {
	if _, err := loginScopes(AccessPublisher, []string{auth.ScopeRemove}, []string{auth.ScopePublish, auth.ScopeRemove}); err == nil {
		t.Fatal("publisher profile accepted an administrative scope")
	}
	if _, err := loginScopes(AccessAdmin, []string{"unknown"}, []string{auth.ScopePublish}); err == nil {
		t.Fatal("accepted an unadvertised scope")
	}
	got, err := loginScopes(AccessAdmin, []string{auth.ScopePublish, auth.ScopePublish}, []string{auth.ScopePublish})
	if err != nil || len(got) != 1 || got[0] != auth.ScopePublish {
		t.Fatalf("deduplicated scopes = %v, %v", got, err)
	}
}
