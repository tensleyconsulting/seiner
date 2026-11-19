package artifactregistry

import (
	"cmp"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

var (
	oidFulcioIssuerV1 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	oidFulcioIssuerV2 = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
)

var signatureFields = []string{
	"signing",
	"identity",
	"issuer",
	"keyHint",
	"source",
	"artifactType",
	"predicateType",
	"manifestDigest",
	"payloadDigest",
	"rekorLogIndex",
	"rekorLogId",
	"integratedTime",
	"certificateValid",
	"serial",
	model.MetaArtifactImage,
	"verified",
}

func isSigTag(tag string) bool {
	return strings.HasSuffix(tag, ".sig")
}

// isSigTagVersion reports whether v represents a cosign .sig tag artifact:
// it has at least one tag and every tag ends in ".sig".
func isSigTagVersion(v Version) bool {
	if len(v.Tags) == 0 {
		return false
	}
	for _, t := range v.Tags {
		if !isSigTag(t) {
			return false
		}
	}
	return true
}

// signedDigestFromTags derives the signed image digest (<algo>:<hex>) from
// cosign .sig tags such as "sha256-<hex>.sig" or "prefix-sha256-<hex>.sig".
func signedDigestFromTags(tags []string) string {
	for _, tag := range tags {
		base, ok := strings.CutSuffix(tag, ".sig")
		if !ok || base == "" {
			continue
		}
		for _, algo := range []string{"sha256", "sha384", "sha512"} {
			marker := algo + "-"
			if hex, ok := strings.CutPrefix(base, marker); ok && isHex(hex) {
				return algo + ":" + hex
			}
			if _, after, ok := strings.Cut(base, "-"+marker); ok && isHex(after) {
				return algo + ":" + after
			}
		}
	}
	return ""
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func signatureResource(project string, parent model.Resource, image string, s Signature) model.Resource {
	md := map[string]string{
		model.MetaArtifactImage: image,
		"verified":              "no (seiner does not verify signatures)",
	}
	cert, err := parseCert(s.CertificatePEM)
	hasCert := err == nil && cert != nil

	var identity, fulcioIssuer string
	if hasCert {
		identity = certIdentity(cert)
		fulcioIssuer = fulcioOIDCIssuer(cert)
	}

	var signing, badge string
	switch {
	case s.CertificatePEM != "" && fulcioIssuer != "":
		signing, badge = "keyless", "KEYLESS"
	case s.CertificatePEM != "":
		signing, badge = "certificate", "CERT"
	default:
		signing, badge = "key", "KEY"
	}
	md["signing"] = signing
	set(md, "identity", identity)
	if hasCert {
		if fulcioIssuer != "" {
			md["issuer"] = fulcioIssuer
		} else {
			set(md, "issuer", cert.Issuer.String())
		}
		if !cert.NotBefore.IsZero() && !cert.NotAfter.IsZero() {
			md["certificateValid"] = display.Stamp(cert.NotBefore) + " to " + display.Stamp(cert.NotAfter)
		}
		if cert.SerialNumber != nil {
			md["serial"] = cert.SerialNumber.Text(16)
		}
	}
	set(md, "keyHint", s.KeyHint)
	set(md, "source", signatureSource(s))
	set(md, "artifactType", s.ArtifactType)
	set(md, "predicateType", s.PredicateType)
	set(md, "manifestDigest", s.ManifestDigest)
	set(md, "payloadDigest", s.PayloadDigest)
	if s.HasRekor {
		md["rekorLogIndex"] = strconv.FormatInt(s.RekorLogIndex, 10)
	}
	set(md, "rekorLogId", s.RekorLogID)
	set(md, "integratedTime", display.Stamp(s.IntegratedTime))

	var lists []model.List
	if len(s.ChainPEM) > 0 {
		items := make([]string, 0, len(s.ChainPEM))
		for _, p := range s.ChainPEM {
			c, err := parseCert(p)
			if err != nil {
				items = append(items, "not parsed: "+err.Error())
				continue
			}
			items = append(items, fmt.Sprintf("%s · Valid %s to %s", c.Subject.String(), display.Stamp(c.NotBefore), display.Stamp(c.NotAfter)))
		}
		lists = append(lists, model.List{ID: "chain", Label: "Certificate chain", Items: items})
	}

	var copies []model.Copy
	if s.Signature != "" {
		copies = append(copies, model.Copy{Title: "Signature", Label: "signature", Text: s.Signature})
	}
	if s.CertificatePEM != "" {
		copies = append(copies, model.Copy{Title: "Certificate (PEM)", Label: "certificate", Text: joinPEM([]string{s.CertificatePEM})})
	}
	if len(s.ChainPEM) > 0 {
		copies = append(copies, model.Copy{Title: "Certificate chain (PEM)", Label: "certificate chain", Text: joinPEM(s.ChainPEM)})
	}
	if s.BundleJSON != "" {
		copies = append(copies, model.Copy{Title: "Sigstore bundle (JSON)", Label: "Sigstore bundle", Text: s.BundleJSON})
	}

	idSeg := cmp.Or(s.ID, s.ManifestDigest, "signature")
	name := signatureName(s, identity)
	return model.Resource{
		ID:       parent.ID + "/signatures/" + url.PathEscape(idSeg),
		Name:     name,
		Service:  model.ServiceArtifactSignature,
		ScopeID:  "projects/" + project,
		Location: parent.Location,
		Status:   model.StatusUnknown,
		Created:  s.IntegratedTime,
		Metadata: md,
		Fields:   model.FieldsOf(md, signatureFields...),
		Lists:    lists,
		Badge:    badge,
		ParentID: parent.ID,
		FacetID:  model.FacetSignatures,
		Copies:   copies,
	}
}

func signatureName(s Signature, identity string) string {
	if identity != "" {
		return identity
	}
	if s.KeyHint != "" {
		return s.KeyHint
	}
	if s.PayloadDigest != "" {
		return shortenDigest(s.PayloadDigest)
	}
	if s.ManifestDigest != "" {
		return shortenDigest(s.ManifestDigest)
	}
	return cmp.Or(s.ID, "signature")
}

func signatureSource(s Signature) string {
	switch s.Source {
	case "referrer":
		return "OCI referrer"
	case "tag":
		if s.Tag != "" {
			return "tag (" + s.Tag + ")"
		}
		return "tag"
	default:
		return s.Source
	}
}

func compareSignatures(a, b Signature) int {
	az, bz := a.IntegratedTime.IsZero(), b.IntegratedTime.IsZero()
	if az != bz {
		if !az {
			return -1
		}
		return 1
	}
	if !az {
		if c := b.IntegratedTime.Compare(a.IntegratedTime); c != 0 {
			return c
		}
	}
	na := signatureName(a, certIdentityOrEmpty(a.CertificatePEM))
	nb := signatureName(b, certIdentityOrEmpty(b.CertificatePEM))
	if c := model.CompareNatural(na, nb); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

func certIdentityOrEmpty(certPEM string) string {
	cert, err := parseCert(certPEM)
	if err != nil || cert == nil {
		return ""
	}
	return certIdentity(cert)
}

func parseCert(s string) (*x509.Certificate, error) {
	if strings.TrimSpace(s) == "" {
		return nil, errors.New("empty PEM")
	}
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		return nil, errors.New("no PEM block")
	}
	return x509.ParseCertificate(b.Bytes)
}

func certIdentity(cert *x509.Certificate) string {
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}
	if len(cert.URIs) > 0 {
		return cert.URIs[0].String()
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return cmp.Or(cert.Subject.CommonName, cert.Subject.String())
}

func fulcioOIDCIssuer(cert *x509.Certificate) string {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidFulcioIssuerV2) {
			var s string
			if rest, err := asn1.Unmarshal(ext.Value, &s); err == nil && len(rest) == 0 && s != "" {
				return s
			}
			if len(ext.Value) > 0 {
				return string(ext.Value)
			}
		}
	}
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidFulcioIssuerV1) && len(ext.Value) > 0 {
			return string(ext.Value)
		}
	}
	return ""
}

func joinPEM(pems []string) string {
	var b strings.Builder
	for _, p := range pems {
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}
