package actions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tensleyconsulting/seiner/internal/model"
	"github.com/tensleyconsulting/seiner/internal/ui/keys"
)

func sample() model.Resource {
	return model.Resource{
		ID:       "projects/demo-project/buckets/my-app-bucket",
		Name:     "my-app-bucket",
		Service:  model.ServiceCloudStorage,
		ScopeID:  "demo-project",
		Location: "us-central1",
		Status:   model.StatusHealthy,
		Metadata: map[string]string{"storageClass": "STANDARD"},
	}
}

func TestPayload(t *testing.T) {
	t.Parallel()
	r := sample()
	name, err := Payload(r, TargetName)
	if err != nil || name != r.Name {
		t.Fatalf("name %q err=%v", name, err)
	}
	id, err := Payload(r, TargetID)
	if err != nil || id != r.ID {
		t.Fatalf("id %q err=%v", id, err)
	}
	js, err := Payload(r, TargetJSON)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != r.Name {
		t.Fatalf("json %+v", m)
	}
}

func TestCopyModelNav(t *testing.T) {
	t.Parallel()
	m := NewCopy(sample(), keys.Default())
	if m.Selected() != TargetName {
		t.Fatal("default name")
	}
	m.Move(1)
	if m.Selected() != TargetID {
		t.Fatal("id")
	}
	m.GoBottom()
	if m.Selected() != TargetJSON {
		t.Fatal("json")
	}
	m.Move(5)
	if m.Selected() != TargetJSON {
		t.Fatal("clamp")
	}
	m.GoTop()
	if m.Selected() != TargetName {
		t.Fatal("top")
	}
}

func TestCopyView(t *testing.T) {
	t.Parallel()
	m := NewCopy(sample(), keys.Default())
	view := m.View(80, 24, sample())
	for _, s := range []string{"Copy", "Name", "ID", "JSON", "my-app-bucket"} {
		if !strings.Contains(view, s) {
			t.Fatalf("missing %q in\n%s", s, view)
		}
	}
}

func TestCopyExtras(t *testing.T) {
	t.Parallel()
	r := sample()
	r.Copies = []model.Copy{
		{Title: "Attestation", Label: "attestation", Text: "H4sIAAAA"},
		{Title: "Cert chains", Label: "cert chains", Text: "-----BEGIN CERTIFICATE-----\nMIIB\n"},
	}
	m := NewCopy(r, keys.Default())
	m.GoBottom()
	got := m.Selected()
	if Title(r, got) != "Cert chains" || Label(r, got) != "cert chains" {
		t.Fatalf("bottom = %q / %q", Title(r, got), Label(r, got))
	}
	if p, err := Payload(r, got); err != nil || p != r.Copies[1].Text {
		t.Fatalf("payload %q err=%v", p, err)
	}
	m.Move(-1)
	if p, _ := Payload(r, m.Selected()); p != "H4sIAAAA" {
		t.Fatalf("attestation payload %q", p)
	}
	view := m.View(80, 24, r)
	for _, s := range []string{"Attestation", "Cert chains", "H4sIAAAA", "-----BEGIN CERTIFICATE-----"} {
		if !strings.Contains(view, s) {
			t.Fatalf("missing %q in\n%s", s, view)
		}
	}
	if len(Targets(sample())) != 3 {
		t.Fatal("a resource without Copies has only name, ID, JSON")
	}
}

func TestCopyViewDoesNotWrap(t *testing.T) {
	t.Parallel()
	r := sample()
	r.ID = strings.Repeat("projects/demo-project/", 6)
	r.Copies = []model.Copy{{Title: "Cert chains", Label: "cert chains", Text: strings.Repeat("A", 200)}}
	view := ansi.Strip(NewCopy(r, keys.Default()).View(80, 24, r))
	for _, title := range []string{"ID ", "Cert chains "} {
		found := false
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, title) && strings.Contains(line, "…") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q row wrapped instead of truncating:\n%s", title, view)
		}
	}
}
