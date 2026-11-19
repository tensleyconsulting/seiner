package model

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanLineAndText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       string
		wantLine string
		wantText string
	}{
		{
			name:     "plain utf8 untouched",
			in:       "prod-db-01 · us-central1 (100%)",
			wantLine: "prod-db-01 · us-central1 (100%)",
			wantText: "prod-db-01 · us-central1 (100%)",
		},
		{
			name:     "csi color stripped",
			in:       "\x1b[31mCRITICAL\x1b[0m",
			wantLine: "CRITICAL",
			wantText: "CRITICAL",
		},
		{
			name:     "osc 52 clipboard injection stripped",
			in:       "bucket\x1b]52;c;ZXZpbA==\x07-name",
			wantLine: "bucket-name",
			wantText: "bucket-name",
		},
		{
			name:     "kitty apc graphics injection stripped",
			in:       "vm\x1b_Ga=T,f=100;AAAA\x1b\\01",
			wantLine: "vm01",
			wantText: "vm01",
		},
		{
			name:     "c1 csi and osc stripped",
			in:       "a\u009b2Jb\u009d0;title\u009cc",
			wantLine: "abc",
			wantText: "abc",
		},
		{
			name:     "ris terminal reset stripped",
			in:       "\x1bcowned",
			wantLine: "owned",
			wantText: "owned",
		},
		{
			name:     "bidi override stripped",
			in:       "admin\u202etxt.exe\u202c",
			wantLine: "admintxt.exe",
			wantText: "admintxt.exe",
		},
		{
			name:     "newlines and crlf normalized",
			in:       "line1\r\nline2\rline3\tend\x00\x7f",
			wantLine: "line1 line2 line3 end",
			wantText: "line1\nline2\nline3 end",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanLine(tc.in); got != tc.wantLine {
				t.Errorf("cleanLine(%q) = %q, want %q", tc.in, got, tc.wantLine)
			}
			if got := cleanText(tc.in); got != tc.wantText {
				t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.wantText)
			}
		})
	}
}

func TestNormalizeSanitizesUntrustedFields(t *testing.T) {
	t.Parallel()
	pem := "-----BEGIN CERTIFICATE-----\r\nMIIB\x1b]52;c;bad\x07==\r\n-----END CERTIFICATE-----\n"
	r := Normalize(Resource{
		ID:          "projects/p/buckets/b/objects/\x1b[2Jevil",
		Name:        "evil\nname\x1b]52;c;ZXZpbA==\x07",
		Service:     ServiceCloudStorage,
		Location:    "us-east1\r\nspoof",
		State:       "RUNNING\x1b[31m",
		Badge:       "HIGH\u202e",
		Description: "first\r\nsecond\x1b_Gi=1;AAAA\x1b\\",
		Metadata:    map[string]string{"note\nkey": "val\n2\x1b[0m"},
		Labels:      map[string]string{"env": "prod\ninjected"},
		Lists:       []List{{ID: "members", Label: "Members\x07", Items: []string{"user:a@example.com\x1b[32m"}}},
		Copies:      []Copy{{Title: "Chain\n2", Label: "chain\x1b[1m", Text: pem}},
	})
	if strings.ContainsAny(r.Name, "\x1b\n\r") || r.Name != "evil name" {
		t.Errorf("Name = %q", r.Name)
	}
	if r.ID != "projects/p/buckets/b/objects/evil" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Location != "us-east1 spoof" {
		t.Errorf("Location = %q", r.Location)
	}
	if r.State != "RUNNING" {
		t.Errorf("State = %q", r.State)
	}
	if r.Badge != "HIGH" {
		t.Errorf("Badge = %q", r.Badge)
	}
	if r.Description != "first\nsecond" {
		t.Errorf("Description = %q", r.Description)
	}
	if r.Metadata["note key"] != "val\n2" {
		t.Errorf("Metadata = %#v", r.Metadata)
	}
	if r.Labels["env"] != "prod injected" {
		t.Errorf("Labels = %#v", r.Labels)
	}
	if len(r.Fields) != 1 || r.Fields[0] != (Field{Key: "note key", Value: "val\n2"}) {
		t.Errorf("Fields = %#v", r.Fields)
	}
	if r.Lists[0].Label != "Members" || r.Lists[0].Items[0] != "user:a@example.com" {
		t.Errorf("Lists = %#v", r.Lists)
	}
	wantPEM := "-----BEGIN CERTIFICATE-----\nMIIB==\n-----END CERTIFICATE-----\n"
	if r.Copies[0].Title != "Chain 2" || r.Copies[0].Label != "chain" || r.Copies[0].Text != wantPEM {
		t.Errorf("Copies = %#v", r.Copies)
	}
}

func TestNormalizeAllEscapeNameStaysValid(t *testing.T) {
	t.Parallel()
	r := Normalize(Resource{
		ID:      "projects/p/buckets/b/objects/x",
		Name:    "\x1b]52;c;ZXZpbA==\x07",
		Service: ServiceCloudStorage,
	})
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate failed after sanitizing escape-only name: %v", err)
	}
	if r.Name != "(unnamed)" {
		t.Errorf("Name = %q, want (unnamed)", r.Name)
	}
}

func TestBreadcrumbAndErrorsSanitized(t *testing.T) {
	t.Parallel()
	crumb := Breadcrumb([]HierarchyNode{
		{ID: "organizations/1", Name: "Org\x1b[2J"},
		{ID: "projects/p", Name: "Proj\nTwo\u202e"},
	})
	if crumb != "Org / Proj Two" {
		t.Errorf("Breadcrumb = %q", crumb)
	}

	pe := ProviderError{
		Service: ServiceCloudStorage,
		Err:     errors.New("upstream proxy said:\n\x1b]0;pwned\x07bad gateway"),
	}
	if got := pe.Summary(); got != "upstream proxy said: bad gateway" {
		t.Errorf("ProviderError.Summary = %q", got)
	}

	id := Identity{
		Account: "attacker\x1b[31m@example.com",
		Err:     errors.New("oauth:\n\x1b]52;c;bad\x07invalid_grant"),
	}
	if got := id.DisplayAccount(); got != "attacker@example.com" {
		t.Errorf("DisplayAccount = %q", got)
	}
	if got := id.Banner(); got != "auth: oauth: invalid_grant" {
		t.Errorf("Banner = %q", got)
	}
}
