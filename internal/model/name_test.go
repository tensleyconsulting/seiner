package model

import (
	"reflect"
	"testing"
)

func TestParseName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		id   string
		want ParsedName
		ok   bool
	}{
		{"bucket", "projects/p/buckets/b",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "buckets", Name: "b"}, true},
		{"object", "projects/p/buckets/b/objects/logos%2Flogo.png",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "buckets", Name: "b", Rest: []string{"objects", "logos/logo.png"}}, true},
		{"bucket binding", "projects/p/buckets/b/iam/bindings/roles%2Fstorage.admin",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "buckets", Name: "b", Rest: []string{"iam", "bindings", "roles/storage.admin"}}, true},
		{"org binding", "organizations/1/iam/bindings/roles%2Fowner",
			ParsedName{Scope: "organizations/1", Kind: "bindings", Name: "roles/owner"}, true},
		{"folder binding", "folders/2/iam/bindings/prod-admins",
			ParsedName{Scope: "folders/2", Kind: "bindings", Name: "prod-admins"}, true},
		{"network", "projects/p/global/networks/default",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "networks", Name: "default"}, true},
		{"subnet", "projects/p/regions/us-central1/subnetworks/default",
			ParsedName{Scope: "projects/p", Project: "p", Location: "us-central1", Kind: "subnetworks", Name: "default"}, true},
		{"firewall", "projects/p/global/firewalls/allow-ssh",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "firewalls", Name: "allow-ssh"}, true},
		{"instance", "projects/p/zones/us-central1-a/instances/web-1",
			ParsedName{Scope: "projects/p", Project: "p", Location: "us-central1-a", Kind: "instances", Name: "web-1"}, true},
		{"located", "projects/p/locations/us-central1/clusters/c",
			ParsedName{Scope: "projects/p", Project: "p", Location: "us-central1", Kind: "clusters", Name: "c"}, true},
		{"enabled service", "projects/p/services/storage.googleapis.com",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "services", Name: "storage.googleapis.com"}, true},
		{"project", "projects/p",
			ParsedName{Scope: "projects/p", Project: "p", Kind: "projects", Name: "p"}, true},
		{"folder", "folders/2",
			ParsedName{Scope: "folders/2", Kind: "folders", Name: "2"}, true},
		{"org", "organizations/1",
			ParsedName{Scope: "organizations/1", Kind: "organizations", Name: "1"}, true},
		{"bare id", "demo-project", ParsedName{}, false},
		{"empty scope id", "projects//buckets/b", ParsedName{}, false},
		{"no leaf", "projects/p/buckets", ParsedName{}, false},
		{"location without leaf", "projects/p/zones/z", ParsedName{}, false},
		{"bad escape", "projects/p/buckets/b%zz", ParsedName{}, false},
		{"synthetic root", "cloud/root", ParsedName{}, false},
		{"empty", "", ParsedName{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseName(tc.id)
			if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseName(%q) = %+v %v want %+v %v", tc.id, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParsedNameScopeParts(t *testing.T) {
	t.Parallel()
	p, _ := ParseName("folders/2/policies/x")
	if p.ScopeCollection() != "folders" || p.ScopeID() != "2" {
		t.Fatalf("%q %q", p.ScopeCollection(), p.ScopeID())
	}
}
