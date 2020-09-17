package main

import (
	"testing"

	"github.com/cmaster11/overseer/test"
	"gopkg.in/yaml.v2"
)

func TestNewResultFilterFromQuery(t *testing.T) {

	testSyntaxOK := func(t *testing.T, query string) {
		filter, err := newResultFilterFromQuery(query)
		if err != nil {
			t.Fatalf("bad query: %s, %s", query, err)
		}
		m, _ := yaml.Marshal(filter)
		t.Logf("query: %s, filter:\n%s", query, string(m))
	}
	testSyntaxBad := func(t *testing.T, query string) {
		if _, err := newResultFilterFromQuery(query); err == nil {
			t.Fatalf("should have been bad query: %s", query)
		}
	}

	// Simple elements
	testSyntaxOK(t, "isDedup=true")
	testSyntaxOK(t, "recovered=true")
	testSyntaxOK(t, "type=a.*")
	testSyntaxOK(t, "tag=a.*")
	testSyntaxOK(t, "testLabel=My\\slabel.*")
	testSyntaxOK(t, "input=a.*")
	testSyntaxOK(t, "target=a.*")
	testSyntaxOK(t, "error=a.*")
	testSyntaxOK(t, "details=a.*")

	// Combined
	testSyntaxOK(t, "error=a.*,input=a.*,isDedup=false")
	testSyntaxOK(t, "error=a\\,.*,input=a.*")

	// Invalid
	testSyntaxBad(t, "errors=asdasd")
	testSyntaxBad(t, "error=asd**")
	testSyntaxBad(t, "error=asd*,,isDedup=true")

	testMatchOK := func(t *testing.T, query string, result *test.Result) {
		filter, err := newResultFilterFromQuery(query)
		if err != nil {
			t.Fatalf("bad query: %s, %s", query, err)
		}
		if !filter.Matches(result) {
			t.Fatalf("bad match. query: %s, filter: %+v, %s", query, filter, err)
		}
		m, _ := yaml.Marshal(filter)
		t.Logf("query: %s, result:%+v, filter:\n%s", query, result, string(m))
	}
	testMatchBad := func(t *testing.T, query string, result *test.Result) {
		filter, err := newResultFilterFromQuery(query)
		if err != nil {
			t.Fatalf("bad query: %s, %s", query, err)
		}
		if filter.Matches(result) {
			t.Fatalf("should not match. query: %s, filter: %+v, %s", query, filter, err)
		}
		m, _ := yaml.Marshal(filter)
		t.Logf("no match. query: %s, result:%+v, filter:\n%s", query, result, string(m))
	}

	// Simple elements
	testMatchOK(t, "isDedup=true", &test.Result{IsDedup: true})
	testMatchOK(t, "recovered=true", &test.Result{Recovered: true})
	testMatchOK(t, "type=a.*", &test.Result{Type: "asd"})
	testMatchOK(t, "tag=a.*", &test.Result{Tag: "a2"})
	testLabel := "My label 123"
	testMatchOK(t, "testLabel=My\\slabel.*", &test.Result{TestLabel: &testLabel})
	testMatchOK(t, "input=a.*", &test.Result{Input: "aaaaa"})
	testMatchOK(t, "target=a.*", &test.Result{Target: "aaaa"})
	errAAA := "oaaa"
	testMatchOK(t, "error=a.*", &test.Result{Error: &errAAA})
	detailsAAA := "oaaa"
	testMatchOK(t, "details=a.*", &test.Result{Details: &detailsAAA})

	testMatchBad(t, "error=^a.*", &test.Result{Error: &errAAA})
	testMatchBad(t, "error=^a.*", &test.Result{Error: nil})

	testMatchOK(t, "input=a.*,tag=^my-cluster", &test.Result{Input: "aaaaa", Tag: "my-cluster-123"})
	testMatchBad(t, "input=a.*,tag=^my-cluster$", &test.Result{Input: "aaaaa", Tag: "my-cluster-123"})

	// Inverse
	testMatchOK(t, "input=a.*,tag=!my-cluster", &test.Result{Input: "aaaaa", Tag: "mx-cluster"})
	testMatchBad(t, "input=a.*,tag=!my-cluster", &test.Result{Input: "aaaaa", Tag: "my-cluster"})
}
