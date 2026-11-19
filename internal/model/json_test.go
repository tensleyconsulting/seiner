package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResourceJSONFieldSet(t *testing.T) {
	t.Parallel()
	r := Resource{
		ID:          "projects/demo/buckets/my-app-bucket",
		Name:        "my-app-bucket",
		Service:     ServiceCloudStorage,
		ScopeID:     "demo",
		Location:    "us-central1",
		Status:      StatusHealthy,
		Description: "Application object storage",
		Metadata:    map[string]string{"storageClass": "STANDARD"},
		ParentID:    "projects/demo/buckets/parent",
		Copies:      []Copy{{Title: "Attestation", Label: "attestation", Text: "H4sIAAAAAAAA"}},
	}
	s, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"id": true, "name": true, "service": true,
		"scopeId": true, "location": true, "status": true,
		"description": true, "metadata": true, "parentId": true,
	}
	for k := range m {
		if !want[k] {
			t.Fatalf("unexpected field %q leaked into JSON:\n%s", k, s)
		}
	}
	if m["id"] != r.ID || m["name"] != r.Name {
		t.Fatalf("identity: %+v", m)
	}
	if m["service"] != string(ServiceCloudStorage) {
		t.Fatalf("service %v", m["service"])
	}
	if m["parentId"] != r.ParentID {
		t.Fatalf("parentId %v", m["parentId"])
	}
	if strings.Contains(s, "H4sI") {
		t.Fatalf("copy text leaked into JSON:\n%s", s)
	}
}

func TestResourceJSONIncludesRicherFields(t *testing.T) {
	t.Parallel()
	r := Resource{
		ID:      "projects/demo/zones/z/instances/web-1",
		Name:    "web-1",
		Service: ServiceComputeEngine,
		State:   "RUNNING",
		Created: time.Date(2025, 1, 15, 18, 4, 12, 0, time.FixedZone("PST", -8*3600)),
		Labels:  map[string]string{"env": "prod"},
		Fields:  []Field{{"machineType", "e2-medium"}, {"internalIP", "10.0.0.2"}},
	}
	s, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, `"raw"`) {
		t.Fatalf("raw leaked:\n%s", s)
	}
	var got struct {
		State   string            `json:"state"`
		Created string            `json:"created"`
		Labels  map[string]string `json:"labels"`
		Fields  []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatal(err)
	}
	if got.State != "RUNNING" || got.Created != "2025-01-16T02:04:12Z" || got.Labels["env"] != "prod" {
		t.Fatalf("%+v", got)
	}
	if len(got.Fields) != 2 || got.Fields[0].Key != "machineType" || got.Fields[1].Key != "internalIP" {
		t.Fatalf("fields %+v", got.Fields)
	}
}

func TestResourceJSONOmitsZeroCreated(t *testing.T) {
	t.Parallel()
	s, err := Resource{ID: "i", Name: "n", Service: ServiceGKE}.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, `"created"`) {
		t.Fatalf("zero created encoded:\n%s", s)
	}
}
