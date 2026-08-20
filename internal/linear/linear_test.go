package linear

// linear_test.go covers which token a read is made with — the one thing scoping
// changes, since what a token can see is Linear's own grant and not something
// this package narrows — and then the read itself: the decode, the field
// mapping, and each of the four ways a call fails.
//
// The failures matter as much as the success. The collector drops a read that
// errored, and that error is the only thing separating a revoked token from a
// product with nothing assigned; a call that swallowed one would put an empty
// backlog on the screen and call it the truth.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// ---- the read itself --------------------------------------------------------
//
// Everything below the one HTTP call — the decode, the GraphQL error, the field
// mapping onto Issue — was unreachable while the endpoint was compiled in, so
// the package's only covered line was "an empty key reads nothing". These point
// a read at an httptest server instead.

// serve points the package's endpoint at a handler for the duration of a test.
func serve(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := endpoint
	endpoint = srv.URL
	t.Cleanup(func() {
		endpoint = prev
		srv.Close()
	})
}

const okBody = `{"data":{"viewer":{"assignedIssues":{"nodes":[
  {"id":"uuid-1","identifier":"ENG-124","title":"Fix the thing",
   "description":"it is broken","priorityLabel":"Urgent",
   "updatedAt":"2026-08-19T10:00:00Z","state":{"name":"In Progress"},
   "team":{"key":"ENG"}},
  {"id":"uuid-2","identifier":"ENG-125","title":"Second",
   "description":"","priorityLabel":"Medium",
   "updatedAt":"2026-08-18T09:30:00Z","state":{"name":"Todo"},
   "team":{"key":"ENG"}}]}}}}`

// The mapping is the whole of what this package produces: priorityLabel becomes
// Priority, the nested state and team become flat strings, and the id and the
// identifier stay distinct — the collector dedupes on one and keys the picked
// set by the other, so collapsing them would be a bug nothing else could catch.
func TestAssignedDecodesAndMaps(t *testing.T) {
	var gotAuth, gotType, gotMethod string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType, gotMethod = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Method
		_, _ = io.WriteString(w, okBody)
	})

	issues, err := Assigned("lin_api_acme")
	if err != nil {
		t.Fatal(err)
	}
	// The key reached the wire as the raw header, which is the whole of the
	// scope: this read is acme's rather than anyone else's because of it.
	if gotAuth != "lin_api_acme" {
		t.Errorf("Authorization on the wire = %q", gotAuth)
	}
	if gotMethod != http.MethodPost || gotType != "application/json" {
		t.Errorf("request was %s %s", gotMethod, gotType)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}
	first := issues[0]
	if first.ID != "uuid-1" || first.Identifier != "ENG-124" {
		t.Errorf("id/identifier must stay distinct: %+v", first)
	}
	if first.Title != "Fix the thing" || first.Description != "it is broken" {
		t.Errorf("issue = %+v", first)
	}
	if first.Priority != "Urgent" {
		t.Errorf("Priority = %q, want the priorityLabel", first.Priority)
	}
	if first.State != "In Progress" || first.Team != "ENG" {
		t.Errorf("state/team = %q/%q", first.State, first.Team)
	}
	if !first.UpdatedAt.Equal(time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("UpdatedAt = %v", first.UpdatedAt)
	}
}

// An empty node list is a product with nothing assigned, and must not read as a
// failure — the collector tells "no signal" from "nothing open" by the error.
func TestAssignedEmptyIsNotAnError(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"viewer":{"assignedIssues":{"nodes":[]}}}}`)
	})
	issues, err := Assigned("lin_api_acme")
	if err != nil {
		t.Fatalf("an empty backlog is not an error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("got %d issues", len(issues))
	}
}

// A revoked or wrongly-scoped key comes back as a GraphQL error inside a 200.
// It must surface as an error, because the collector drops a failed read and
// that is the only thing keeping a bad token from reading as an empty product.
func TestAssignedGraphQLErrorIsAnError(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"Authentication required"}],"data":null}`)
	})
	issues, err := Assigned("lin_api_revoked")
	if err == nil {
		t.Fatal("a GraphQL error must not read as an empty backlog")
	}
	if err.Error() != "Authentication required" {
		t.Errorf("err = %q, want Linear's own message", err)
	}
	if issues != nil {
		t.Errorf("a failed read must carry no issues, got %v", issues)
	}
}

// A 500 behind a proxy is HTML, not JSON. It has to degrade the same way.
func TestAssignedMalformedBodyIsAnError(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "<html>gateway error</html>")
	})
	if _, err := Assigned("lin_api_acme"); err == nil {
		t.Error("an unparseable body must be an error, not an empty backlog")
	}
}

// And a transport failure — the machine offline, the endpoint refusing.
func TestAssignedTransportFailureIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now
	prev := endpoint
	endpoint = url
	t.Cleanup(func() { endpoint = prev })

	if _, err := Assigned("lin_api_acme"); err == nil {
		t.Error("a dead endpoint must be an error, not an empty backlog")
	}
}
