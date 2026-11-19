package kms

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// testCert makes a self-signed PEM certificate valid until notAfter.
func testCert(t *testing.T, cn string, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(0xabc),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func hsmVersion(t *testing.T) model.Resource {
	t.Helper()
	keyID := ringID("p", "us-central1", "app-keys") + "/cryptoKeys/signing"
	r := versionResource("us-central1", keyID, KeyVersion{Name: "2", State: "ENABLED", ProtectionLevel: "HSM"}, false)
	r.ScopeID = "projects/p"
	return r
}

func TestVersionAttestationFacet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level string
		want  bool
	}{
		{"HSM", true},
		{"SOFTWARE", false},
		{"EXTERNAL", false},
	}
	for _, tt := range tests {
		r := versionResource("l", "k", KeyVersion{Name: "1", ProtectionLevel: tt.level}, false)
		f, ok := r.InlineFacet()
		if ok != tt.want || (ok && f.ID != model.FacetAttestation) {
			t.Errorf("%s: facets %+v", tt.level, r.Facets)
		}
	}
}

func TestAttestation(t *testing.T) {
	t.Parallel()
	future := time.Now().Add(24 * time.Hour)
	leaf, root := testCert(t, "partition leaf", future), testCert(t, "old root", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	card := testCert(t, "card", future)
	content := []byte("\x1f\x8b\x08\x00attestation")
	api := sample()
	api.attest = map[string]Attestation{"signing/2": {
		Format:          "CAVIUM_V2_COMPRESSED",
		Content:         content,
		Cavium:          []string{leaf, root},
		GoogleCard:      []string{card},
		GooglePartition: []string{"not a certificate"},
	}}
	parent := hsmVersion(t)
	got, truncated, err := New(api).ListRelated(context.Background(), parent, model.FacetAttestation)
	if err != nil || truncated {
		t.Fatalf("truncated=%v err=%v", truncated, err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows, want attestation and three chains: %+v", len(got), got)
	}
	a := got[0]
	if a.ID != parent.ID+"/attestation" || a.Service != model.ServiceKMSAttestation || a.ParentID != parent.ID ||
		a.FacetID != model.FacetAttestation || a.ScopeID != "projects/p" || a.Badge != "V2" ||
		a.Metadata["size"] != "15 bytes" || a.Metadata["certificates"] != "Cavium 2, Google card 1, Google partition 1" ||
		!strings.HasPrefix(a.Metadata["verified"], "no") {
		t.Fatalf("attestation row: %+v", a)
	}
	if len(a.Copies) != 2 || a.Copies[0].Text != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("attestation copies: %+v", a.Copies)
	}
	if bundle := a.Copies[1].Text; strings.Count(bundle, "BEGIN CERTIFICATE") != 3 || !strings.HasSuffix(bundle, "not a certificate\n") {
		t.Fatalf("bundle = %q", bundle)
	}
	if js, _ := a.JSON(); strings.Contains(js, a.Copies[0].Text) {
		t.Fatal("attestation content leaked into JSON")
	}

	cav := got[1]
	if cav.Name != "Cavium" || cav.Service != model.ServiceKMSCertChain || cav.ID != parent.ID+"/attestation/cavium" ||
		cav.Status != model.StatusWarning || cav.Badge != "EXPIRED" || cav.Metadata["certificates"] != "2" ||
		cav.Metadata["expires"] != "2021-01-01 00:00 UTC" || len(cav.Lists) != 2 {
		t.Fatalf("cavium chain: %+v", cav)
	}
	if items := cav.List("cert-1"); len(items) != 4 || !strings.Contains(items[0], "CN=partition leaf") || !strings.Contains(items[2], "abc") {
		t.Fatalf("leaf items: %q", items)
	}
	if cav.Copies[0].Text != leaf+root {
		t.Fatalf("cavium PEM = %q", cav.Copies[0].Text)
	}
	if c := got[2]; c.Name != "Google card" || c.Status != model.StatusUnknown || c.Badge != "" {
		t.Fatalf("card chain: %+v", c)
	}
	if p := got[3]; len(p.Lists) != 1 || !strings.HasPrefix(p.Lists[0].Items[0], "not parsed") {
		t.Fatalf("partition chain: %+v", p)
	}
}

func TestAttestationMissingChainAndNone(t *testing.T) {
	t.Parallel()
	api := sample()
	api.attest = map[string]Attestation{"signing/2": {Format: "CAVIUM_V1_COMPRESSED", Content: []byte{1}}}
	got, _, err := New(api).ListRelated(context.Background(), hsmVersion(t), model.FacetAttestation)
	if err != nil || len(got) != 1 || len(got[0].Copies) != 1 || got[0].Badge != "V1" {
		t.Fatalf("no chains: %+v err=%v", got, err)
	}
	api.attest = nil
	got, _, err = New(api).ListRelated(context.Background(), hsmVersion(t), model.FacetAttestation)
	if err != nil || len(got) != 0 {
		t.Fatalf("no attestation: %+v err=%v", got, err)
	}
}

func TestAttestationErrors(t *testing.T) {
	t.Parallel()
	api := sample()
	api.err = errors.New("boom")
	if _, _, err := New(api).ListRelated(context.Background(), hsmVersion(t), model.FacetAttestation); err == nil {
		t.Fatal("API error not returned")
	}
	key := ringResource("p", sample().rings["us-central1"][0])
	key.Service = model.ServiceKMSKeyVersion
	got, _, err := New(api).ListRelated(context.Background(), key, model.FacetAttestation)
	if err != nil || got != nil {
		t.Fatalf("a non-version ID lists nothing: %+v err=%v", got, err)
	}
}
