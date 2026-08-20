package linear

// linear_test.go covers the one thing scoping a read changes: which token it is
// made with. What that token can see — the workspace, and the teams in it — is
// Linear's own grant, not something this package narrows. The transport is left
// alone; a failed call is "no signal" by design.

import (
	"encoding/json"
	"io"
	"testing"
)

func TestNewRequestCarriesTheKey(t *testing.T) {
	req, err := newRequest("lin_api_acme")
	if err != nil {
		t.Fatal(err)
	}
	// The key is the scope: this is the whole of what makes the read one
	// product's rather than another's.
	if got := req.Header.Get("Authorization"); got != "lin_api_acme" {
		t.Errorf("Authorization = %q, want the raw key", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Query == "" {
		t.Error("request carries no query")
	}
}

func TestAssignedWithoutKeyReadsNothing(t *testing.T) {
	// No token is not an error — it is a source that was never configured, and
	// the collector must be able to tell the two apart.
	issues, err := Assigned("")
	if err != nil || issues != nil {
		t.Errorf("Assigned(\"\") = %v, %v; want nil, nil", issues, err)
	}
}

func TestKeyReadsTheAmbientEnv(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "lin_api_ambient")
	if got := Key(); got != "lin_api_ambient" {
		t.Errorf("Key() = %q", got)
	}
	t.Setenv("LINEAR_API_KEY", "")
	if got := Key(); got != "" {
		t.Errorf("Key() with no env = %q, want empty", got)
	}
}
