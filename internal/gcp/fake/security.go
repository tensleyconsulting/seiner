package fake

import (
	"strings"

	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	certPrefix = "projects/demo-project/locations/global/"
	certMapID  = certPrefix + "certificateMaps/web-map"
)

// certFixtures is Certificate Manager: three certificates in different
// states, a map serving them, and a DNS authorization, issuance config, and
// trust config.

func certFixtures() []model.Resource {
	cert := func(name, desc string, st model.ResourceStatus, state, badge, created string, md map[string]string, sans []string) model.Resource {
		r := res(certPrefix+"certificates/"+name, name, model.ServiceCertificate, "projects/demo-project", "global", st, desc, md)
		r.Lists = []model.List{{ID: "sans", Label: "Subject alternative names", Items: sans}}
		r.Badge = badge
		return extra(ordered(r, "type", "scope", "domains", "expires", "issue", "dnsAuthorizations", "issuanceConfig", "identity", "updated"), state, created, map[string]string{"env": "prod"})
	}
	webCert := cert("web-cert", "Public site", model.StatusHealthy, "ACTIVE", "", "2025-03-10T09:00:00Z",
		map[string]string{"type": "Google-managed", "domains": "example.com, www.example.com", "expires": "2026-12-20 09:00 UTC", "dnsAuthorizations": "example-com"},
		[]string{"example.com", "www.example.com"})
	webCert.Lists = append(webCert.Lists, model.List{ID: "usedBy", Label: "Used by", Items: []string{"//certificatemanager.googleapis.com/" + certMapID + "/certificateMapEntries/www"}})
	entry := func(name, host, matcher, certs string) model.Resource {
		label := host
		if label == "" {
			label = matcher
		}
		md := map[string]string{"entry": name, "certificates": certs}
		if host != "" {
			md["hostname"] = host
		}
		if matcher != "" {
			md["matcher"] = matcher
		}
		r := res(certMapID+"/certificateMapEntries/"+name, label, model.ServiceCertMapEntry, "projects/demo-project", "global", model.StatusHealthy, "", md)
		r.Badge = matcher
		return under(extra(ordered(r, "hostname", "matcher", "certificates", "entry", "updated"), "ACTIVE", "2025-03-10T09:10:00Z", nil), certMapID, model.FacetMapEntries)
	}
	webMap := res(certMapID, "web-map", model.ServiceCertificateMap, "projects/demo-project", "global", "", "Certificates for the public HTTPS frontend", map[string]string{"targets": "web-proxy", "addresses": "203.0.113.110:443"})
	webMap.Facets = []model.Facet{{ID: model.FacetMapEntries, Label: "Entries", Inline: true}}
	return []model.Resource{
		webCert,
		cert("legacy-cert", "Uploaded certificate for the old API host", model.StatusWarning, "", "12d", "2024-11-08T14:00:00Z",
			map[string]string{"type": "self-managed", "expires": "2026-11-08 14:00 UTC"}, []string{"api.example.com"}),
		cert("status-cert", "Status page", model.StatusWarning, "PROVISIONING", "", "2026-10-26T16:00:00Z",
			map[string]string{"type": "Google-managed", "domains": "status.example.com", "issue": "AUTHORIZATION_ISSUE: DNS record not found"},
			[]string{"status.example.com"}),
		extra(ordered(webMap, "targets", "addresses", "updated"), "", "2025-03-10T09:05:00Z", map[string]string{"env": "prod"}),
		entry("primary", "", "PRIMARY", "web-cert"),
		entry("www", "www.example.com", "", "web-cert"),
		entry("api", "api.example.com", "", "legacy-cert"),
		extra(ordered(res(certPrefix+"dnsAuthorizations/example-com", "example-com", model.ServiceDNSAuthorization, "projects/demo-project", "global", "", "", map[string]string{"domain": "example.com", "type": "fixed record", "recordName": "_acme-challenge.example.com.", "recordType": "CNAME", "recordData": "0f3c9b2e-7a1d-4e5f-8b6c-2d9e1a4f7c30.4.authorize.certificatemanager.goog."}), "domain", "type", "recordName", "recordType", "recordData", "updated"), "", "2025-03-10T08:50:00Z", nil),
		extra(ordered(res(certPrefix+"certificateIssuanceConfigs/internal-ca", "internal-ca", model.ServiceIssuanceConfig, "projects/demo-project", "global", "", "Private certificates for internal hosts", map[string]string{"caPool": "projects/demo-project/locations/us-central1/caPools/internal-pool", "keyAlgorithm": "ECDSA_P256", "lifetime": "720h0m0s", "rotationWindow": "66%"}), "caPool", "keyAlgorithm", "lifetime", "rotationWindow", "updated"), "", "2025-05-02T11:00:00Z", nil),
		extra(ordered(res(certPrefix+"trustConfigs/mtls-clients", "mtls-clients", model.ServiceTrustConfig, "projects/demo-project", "global", "", "Client CAs for partner mTLS", map[string]string{"trustAnchors": "1", "intermediates": "1"}), "trustAnchors", "intermediates", "allowlisted", "spiffeTrustDomains", "updated"), "", "2025-07-14T15:20:00Z", nil),
	}
}

func privatecaFixtures() []model.Resource {
	const (
		loc            = "us-central1"
		prefix         = "projects/demo-project/locations/" + loc + "/"
		internalPoolID = prefix + "caPools/internal-pool"
		swpPoolID      = prefix + "caPools/swp-pool"
		rootCAID       = internalPoolID + "/certificateAuthorities/root-ca-1"
		pendingSubID   = internalPoolID + "/certificateAuthorities/pending-sub-ca"
		swpSubCAID     = swpPoolID + "/certificateAuthorities/swp-sub-ca"
		mtlsTemplateID = prefix + "certificateTemplates/mtls-leaf"
		swpTemplateID  = prefix + "certificateTemplates/swp-inspection"
		placeholder    = "demo fixture: not a real PEM document\n"
	)

	poolFields := []string{"tier", "publishCaCert", "publishCrl", "encoding", "allowedKeyTypes",
		"maxLifetime", "backdate", "issuanceModes", "identityPassthrough", "identityExpression",
		"passthroughExtensions", "kmsKey"}
	caFields := []string{"type", "tier", "subject", "issuer", "validFrom", "expires", "lifetime",
		"algorithm", "kmsKeyVersion", "serial", "sha256", "gcsBucket", "caCertUrl",
		"deleted", "purgeTime", "updated"}
	certFields := []string{"subject", "sans", "issuerCa", "template", "subjectMode",
		"validFrom", "expires", "lifetime", "serial", "sha256",
		"revocationReason", "revokedAt", "updated"}
	crlFields := []string{"sequenceNumber", "revisionId", "revokedCount", "accessUrl", "updated"}
	tmplFields := []string{"maxLifetime", "isCa", "keyUsage", "extendedKeyUsage",
		"identityPassthrough", "identityExpression", "passthroughExtensions", "updated"}

	internalPool := res(internalPoolID, "internal-pool", model.ServiceCAPool, "projects/demo-project", loc, "", "",
		map[string]string{"tier": "ENTERPRISE", "publishCaCert": "on", "publishCrl": "on", "encoding": "PEM",
			"allowedKeyTypes": "ECDSA_P256, RSA 2048–4096", "maxLifetime": "365 days", "issuanceModes": "CSR, config",
			"identityPassthrough": "subject, SANs", "kmsKey": "projects/demo-project/locations/us-central1/keyRings/app-keys/cryptoKeys/secrets"})
	internalPool.Badge = "ENTERPRISE"
	internalPool.Labels = map[string]string{"env": "prod", "team": "security"}
	internalPool.Facets = []model.Facet{
		{ID: model.FacetCertificateAuthorities, Label: "Certificate Authorities"},
		{ID: model.FacetCertificates, Label: "Certificates"},
		{ID: model.FacetPermissions, Label: "Permissions"},
	}

	swpPool := res(swpPoolID, "swp-pool", model.ServiceCAPool, "projects/demo-project", loc, "", "",
		map[string]string{"tier": "DEVOPS", "publishCaCert": "on", "allowedKeyTypes": "ECDSA_P256", "maxLifetime": "30 days"})
	swpPool.Badge = "DEVOPS"
	swpPool.Labels = map[string]string{"env": "prod", "team": "platform"}
	swpPool.Facets = []model.Facet{
		{ID: model.FacetCertificateAuthorities, Label: "Certificate Authorities"},
		{ID: model.FacetPermissions, Label: "Permissions"},
	}

	rootCA := res(rootCAID, "root-ca-1", model.ServiceCertificateAuthority, "projects/demo-project", loc, model.StatusHealthy, "",
		map[string]string{"type": "root (self-signed)", "tier": "ENTERPRISE", "subject": "CN=Example Internal Root CA, O=Example Corp, C=US",
			"validFrom": "2024-05-01 00:00 UTC", "expires": "2034-05-01 00:00 UTC", "lifetime": "3650 days",
			"algorithm": "EC_P384_SHA384", "serial": "01a2b3c4d5e6", "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			"gcsBucket": "demo-project-cas-public", "caCertUrl": "http://demo-project-cas-public.storage.googleapis.com/ca.crt"})
	rootCA.Badge = "ROOT"
	rootCA.Lists = []model.List{{ID: "crlUrls", Label: "CRL URLs", Items: []string{"http://demo-project-cas-public.storage.googleapis.com/crl.crl"}}}
	rootCA.Facets = []model.Facet{{ID: model.FacetCRLs, Label: "Revocation Lists", Inline: true}}
	rootCA.Copies = []model.Copy{{Title: "CA certificate (PEM)", Label: "CA certificate", Text: placeholder}}

	pendingSub := res(pendingSubID, "pending-sub-ca", model.ServiceCertificateAuthority, "projects/demo-project", loc, model.StatusWarning, "",
		map[string]string{"type": "subordinate", "tier": "ENTERPRISE", "subject": "CN=Example Partner Issuing CA, O=Example Corp, C=US",
			"lifetime": "1095 days", "algorithm": "RSA_PSS_4096_SHA256"})
	pendingSub.Badge = "AWAITING_USER_ACTIVATION"

	swpSub := res(swpSubCAID, "swp-sub-ca", model.ServiceCertificateAuthority, "projects/demo-project", loc, model.StatusHealthy, "",
		map[string]string{"type": "subordinate", "tier": "DEVOPS", "subject": "CN=SWP Egress Inspection CA, O=Example Corp",
			"issuer": "internal-pool/root-ca-1", "validFrom": "2025-06-01 08:30 UTC", "expires": "2028-06-01 08:30 UTC",
			"lifetime": "1095 days", "algorithm": "EC_P256_SHA256", "serial": "778899aabbcc"})
	swpSub.Badge = "SUB"
	swpSub.Copies = []model.Copy{
		{Title: "CA certificate (PEM)", Label: "CA certificate", Text: placeholder},
		{Title: "CA chain (PEM)", Label: "CA chain", Text: placeholder},
	}

	activeCert := res(internalPoolID+"/certificates/checkout-mtls-202610", "checkout-mtls-202610", model.ServiceCACertificate, "projects/demo-project", loc, model.StatusHealthy, "",
		map[string]string{"subject": "CN=checkout-api.corp.internal, O=Example Corp", "sans": "checkout-api.corp.internal, spiffe://demo-project.svc.id.goog/ns/checkout/sa/api",
			"issuerCa": "root-ca-1", "template": "mtls-leaf", "validFrom": "2026-10-15 09:00 UTC", "expires": "2026-12-14 09:00 UTC",
			"lifetime": "60 days", "serial": "4e1f2a3b9c8d"})
	activeCert.Lists = []model.List{{ID: "sans", Label: "Subject alternative names", Items: []string{"checkout-api.corp.internal", "spiffe://demo-project.svc.id.goog/ns/checkout/sa/api"}}}
	activeCert.Copies = []model.Copy{
		{Title: "Certificate (PEM)", Label: "certificate", Text: placeholder},
		{Title: "Certificate chain (PEM)", Label: "certificate chain", Text: placeholder},
	}

	revokedCert := res(internalPoolID+"/certificates/legacy-worker-202604", "legacy-worker-202604", model.ServiceCACertificate, "projects/demo-project", loc, model.StatusError, "",
		map[string]string{"subject": "CN=legacy-worker.corp.internal, O=Example Corp", "sans": "legacy-worker.corp.internal",
			"issuerCa": "root-ca-1", "template": "mtls-leaf", "validFrom": "2026-04-01 00:00 UTC", "expires": "2027-04-01 00:00 UTC",
			"lifetime": "365 days", "serial": "1029384756ab", "revocationReason": "KEY_COMPROMISE", "revokedAt": "2026-09-12 14:20 UTC"})
	revokedCert.Badge = "REVOKED"
	revokedCert.Lists = []model.List{{ID: "sans", Label: "Subject alternative names", Items: []string{"legacy-worker.corp.internal"}}}
	revokedCert.Copies = []model.Copy{{Title: "Certificate (PEM)", Label: "certificate", Text: placeholder}}

	crlActive := res(rootCAID+"/certificateRevocationLists/00000002", "00000002", model.ServiceCertificateRevocationList, "projects/demo-project", loc, model.StatusHealthy, "",
		map[string]string{"sequenceNumber": "2", "revisionId": "e5f60718", "revokedCount": "1",
			"accessUrl": "http://demo-project-cas-public.storage.googleapis.com/crl.crl", "updated": "2026-10-30 00:00 UTC"})
	crlActive.Badge = "ACTIVE"
	crlActive.Lists = []model.List{{ID: "revoked", Label: "Revoked certificates", Items: []string{"1029384756ab  KEY_COMPROMISE  (legacy-worker-202604)"}}}
	crlActive.Copies = []model.Copy{{Title: "CRL (PEM)", Label: "CRL", Text: placeholder}}

	crlOld := res(rootCAID+"/certificateRevocationLists/00000001", "00000001", model.ServiceCertificateRevocationList, "projects/demo-project", loc, model.StatusUnknown, "",
		map[string]string{"sequenceNumber": "1", "revisionId": "a1b2c3d4", "revokedCount": "0", "updated": "2026-09-11 00:00 UTC"})
	crlOld.Badge = "SUPERSEDED"

	mtlsTmpl := res(mtlsTemplateID, "mtls-leaf", model.ServiceCertificateTemplate, "projects/demo-project", loc, "", "Workload mTLS leaf certificates",
		map[string]string{"maxLifetime": "60 days", "keyUsage": "digitalSignature, keyEncipherment", "extendedKeyUsage": "serverAuth, clientAuth",
			"identityPassthrough": "SANs", "identityExpression": "corp.internal and SPIFFE workloads"})
	mtlsTmpl.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Permissions"}}

	swpTmpl := res(swpTemplateID, "swp-inspection", model.ServiceCertificateTemplate, "projects/demo-project", loc, "", "Subordinate CA template for Secure Web Proxy TLS inspection",
		map[string]string{"maxLifetime": "1095 days", "isCa": "yes (max path 1)", "keyUsage": "certSign, crlSign", "identityPassthrough": "subject"})
	swpTmpl.Facets = []model.Facet{{ID: model.FacetPermissions, Label: "Permissions"}}

	return []model.Resource{
		ordered(internalPool, poolFields...),
		ordered(swpPool, poolFields...),
		under(extra(ordered(rootCA, caFields...), "ENABLED", "2024-05-01T00:00:00Z", map[string]string{"role": "root"}), internalPoolID, model.FacetCertificateAuthorities),
		under(extra(ordered(pendingSub, caFields...), "AWAITING_USER_ACTIVATION", "2026-10-29T11:00:00Z", nil), internalPoolID, model.FacetCertificateAuthorities),
		under(extra(ordered(swpSub, caFields...), "ENABLED", "2025-06-01T08:30:00Z", nil), swpPoolID, model.FacetCertificateAuthorities),
		under(extra(ordered(activeCert, certFields...), "ACTIVE", "2026-10-15T09:00:00Z", map[string]string{"service": "checkout"}), internalPoolID, model.FacetCertificates),
		under(extra(ordered(revokedCert, certFields...), "REVOKED", "2026-04-01T00:00:00Z", nil), internalPoolID, model.FacetCertificates),
		under(extra(ordered(crlActive, crlFields...), "ACTIVE", "2026-10-30T00:00:00Z", nil), rootCAID, model.FacetCRLs),
		under(extra(ordered(crlOld, crlFields...), "SUPERSEDED", "2026-09-11T00:00:00Z", nil), rootCAID, model.FacetCRLs),
		extra(ordered(mtlsTmpl, tmplFields...), "", "2025-02-10T10:00:00Z", map[string]string{"team": "security"}),
		extra(ordered(swpTmpl, tmplFields...), "", "2025-05-30T14:00:00Z", nil),
		resourceBinding(internalPoolID, "roles/privateca.admin", "group:security@example.com"),
		resourceBinding(internalPoolID, "roles/privateca.certificateManager", "serviceAccount:service-123456789012@gcp-sa-certificatemanager.iam.gserviceaccount.com"),
		resourceBinding(swpPoolID, "roles/privateca.certificateManager", "serviceAccount:service-123456789012@gcp-sa-networksecurity.iam.gserviceaccount.com"),
		resourceBinding(mtlsTemplateID, "roles/privateca.templateUser", "group:platform@example.com"),
		resourceBinding(swpTemplateID, "roles/privateca.templateUser", "serviceAccount:service-123456789012@gcp-sa-networksecurity.iam.gserviceaccount.com"),
	}
}

const secretPrefix = "projects/demo-project/secrets/"

// secretFix is a secret that expands to its versions. aliases are
// "alias → version" lines.
func secretFix(name string, md map[string]string, created string, labels map[string]string, aliases ...string) model.Resource {
	r := res(secretPrefix+name, name, model.ServiceSecretManager, "projects/demo-project", "global", "", "", md)
	r.Facets = []model.Facet{{ID: model.FacetVersions, Label: "Versions", Inline: true}}
	if len(aliases) > 0 {
		r.Lists = []model.List{{ID: "aliases", Label: "Aliases", Items: aliases}}
	}
	return extra(ordered(r, "type", "replication", "kmsKey", "rotation", "nextRotation", "expires", "topics", "destroyDelay", "annotations"), "", created, labels)
}

func secretVersion(secret, version, state, created string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"ENABLED": model.StatusHealthy, "DISABLED": model.StatusWarning}[state]
	if st == "" {
		st = model.StatusUnknown
	}
	r := res(secretPrefix+secret+"/versions/"+version, version, model.ServiceSecretVersion, "projects/demo-project", "global", st, "", md)
	r.Badge = state
	return under(extra(ordered(r, "destroyed", "scheduledDestroy", "kmsKeyVersion"), state, created, nil), secretPrefix+secret, model.FacetVersions)
}

func kmsRingID(loc, ring string) string {
	return "projects/demo-project/locations/" + loc + "/keyRings/" + ring
}

// kmsRing is a key ring with its keys, import jobs, and permissions.
func kmsRing(loc, name, created string) model.Resource {
	r := res(kmsRingID(loc, name), name, model.ServiceKMS, "projects/demo-project", loc, "", "", nil)
	r.Facets = []model.Facet{
		{ID: model.FacetKeys, Label: "Keys"},
		{ID: model.FacetImportJobs, Label: "Import Jobs"},
		{ID: model.FacetPermissions, Label: "Permissions"},
	}
	return extra(r, "", created, nil)
}

func kmsKeyID(loc, ring, key string) string {
	return kmsRingID(loc, ring) + "/cryptoKeys/" + key
}

// kmsKey is a crypto key; state is its primary version's ("" for keys
// without a primary, which carry a versions summary instead).
func kmsKey(loc, ring, name, state string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"ENABLED": model.StatusHealthy, "DISABLED": model.StatusWarning}[state]
	if state == "" && strings.Contains(md["versions"], "enabled") {
		st = model.StatusHealthy
	}
	r := res(kmsKeyID(loc, ring, name), name, model.ServiceKMSKey, "projects/demo-project", loc, st, "", md)
	r.Badge = map[string]string{"ENCRYPT_DECRYPT": "SYM", "ASYMMETRIC_SIGN": "SIGN"}[md["purpose"]]
	r.Facets = []model.Facet{{ID: model.FacetVersions, Label: "Versions"}, {ID: model.FacetPermissions, Label: "Permissions"}}
	r = ordered(r, "purpose", "algorithm", "protectionLevel", "primaryVersion", "versions", "template", "rotation", "nextRotation", "destroyDelay", "importOnly", "backend", "accessReasons")
	return under(extra(r, state, "2024-04-01T09:00:00Z", nil), kmsRingID(loc, ring), model.FacetKeys)
}

// kmsVersion is a crypto key version; the primary is badged PRIMARY.
func kmsVersion(loc, ring, key, version, state string, primary bool, created string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"ENABLED": model.StatusHealthy, "DISABLED": model.StatusWarning, "DESTROY_SCHEDULED": model.StatusWarning, "IMPORT_FAILED": model.StatusError}[state]
	r := res(kmsKeyID(loc, ring, key)+"/cryptoKeyVersions/"+version, version, model.ServiceKMSKeyVersion, "projects/demo-project", loc, st, "", md)
	r.Badge = state
	if primary {
		r.Metadata["primary"] = "yes"
		r.Badge = "PRIMARY"
	}
	if md["protectionLevel"] == "HSM" {
		r.Facets = []model.Facet{{ID: model.FacetAttestation, Label: "Attestation", Inline: true}}
	}
	r = ordered(r, "primary", "algorithm", "protectionLevel", "generated", "imported", "importJob", "destroyScheduled", "destroyed", "failure", "externalKey", "reimportEligible")
	return under(extra(r, state, created, nil), kmsKeyID(loc, ring, key), model.FacetVersions)
}

// kmsAttestation is an HSM version's attestation row and its three chain
// rows. The copy text is a placeholder: fixtures carry no real attestation
// or certificates.
func kmsAttestation(loc, ring, key, version string) []model.Resource {
	parent := kmsKeyID(loc, ring, key) + "/cryptoKeyVersions/" + version
	placeholder := "demo fixture: not a real attestation"
	a := res(parent+"/attestation", "attestation", model.ServiceKMSAttestation, "projects/demo-project", loc, model.StatusUnknown,
		"Signed by the HSM when it made this version. Copy it as base64 and the chains as PEM to check them with Google's verify_attestation_chains.py.",
		map[string]string{"format": "CAVIUM_V2_COMPRESSED", "size": "1210 bytes", "certificates": "Cavium 2, Google card 2, Google partition 2", "verified": "no (seiner does not verify attestations)"})
	a.Badge = "V2"
	a.Copies = []model.Copy{{Title: "Attestation", Label: "attestation", Text: placeholder}, {Title: "Cert chains", Label: "cert chains", Text: placeholder}}
	out := []model.Resource{under(ordered(a, "format", "size", "certificates", "verified"), parent, model.FacetAttestation)}
	for _, c := range []struct{ id, name, leaf, root string }{
		{"cavium", "Cavium", "CN=Demo HSM card,O=Demo HSM Maker", "CN=Demo HSM Maker Root,O=Demo HSM Maker"},
		{"google-card", "Google card", "CN=Demo card 1001", "CN=Demo Card Root"},
		{"google-partition", "Google partition", "CN=Demo partition 1001-7", "CN=Demo Card Root"},
	} {
		r := res(parent+"/attestation/"+c.id, c.name, model.ServiceKMSCertChain, "projects/demo-project", loc, model.StatusUnknown, "",
			map[string]string{"certificates": "2", "expires": "2034-04-01 00:00 UTC"})
		r.Lists = []model.List{
			{ID: "cert-1", Label: "Certificate 1", Items: []string{"Subject " + c.leaf, "Issuer  " + c.root, "Serial  1a2b", "Valid   2024-04-01 00:00 UTC to 2034-04-01 00:00 UTC"}},
			{ID: "cert-2", Label: "Certificate 2", Items: []string{"Subject " + c.root, "Issuer  " + c.root, "Serial  01", "Valid   2019-01-01 00:00 UTC to 2039-01-01 00:00 UTC"}},
		}
		r.Copies = []model.Copy{{Title: "PEM", Label: c.name + " chain", Text: placeholder}}
		out = append(out, under(ordered(r, "certificates", "expires"), parent, model.FacetAttestation))
	}
	return out
}

func kmsImportJob(loc, ring, name, state, created string, md map[string]string) model.Resource {
	st := map[string]model.ResourceStatus{"ACTIVE": model.StatusHealthy, "PENDING_GENERATION": model.StatusWarning}[state]
	r := res(kmsRingID(loc, ring)+"/importJobs/"+name, name, model.ServiceKMSImportJob, "projects/demo-project", loc, st, "", md)
	r.Badge = state
	r = ordered(r, "method", "protectionLevel", "backend", "generated", "expires", "expired")
	return under(extra(r, state, created, nil), kmsRingID(loc, ring), model.FacetImportJobs)
}

func kmsRingBinding(loc, ring, role string, members ...string) model.Resource {
	return resourceBinding(kmsRingID(loc, ring), role, members...)
}

func kmsKeyBinding(loc, ring, key, role string, members ...string) model.Resource {
	return resourceBinding(kmsKeyID(loc, ring, key), role, members...)
}
