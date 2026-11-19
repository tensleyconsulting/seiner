package privateca

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tensleyconsulting/seiner/internal/gcp/display"
	"github.com/tensleyconsulting/seiner/internal/model"
)

const (
	// maxCertificates caps an Enterprise CA pool's issued certificates. The
	// explorer shows "more not shown" past it.
	maxCertificates = 500
	// maxCRLs caps a Certificate Authority's CRL revisions.
	maxCRLs = 500
	// expiryWarning is how close to expiry a CA or issued certificate turns yellow.
	expiryWarning = 30 * 24 * time.Hour
)

// Pools implements model.ResourceProvider for CA pools, expanding a pool to
// its certificate authorities, issued certificates (Enterprise tier), and IAM
// bindings, and an Enterprise CA to its CRLs.
type Pools struct {
	api API
	now func() time.Time
}

func NewPools(api API) Pools { return Pools{api: api, now: time.Now} }

func (p Pools) Service() model.ServiceType { return model.ServiceCAPool }

// ListPermissions is what List needs. Facet calls check access only when opened.
func (p Pools) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "privateca.caPools.list")
}

// List lists CA pools across every Private CA location.
func (p Pools) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("privateca: no API")
	}
	pools, err := p.api.ListPools(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(pools))
	for _, pool := range pools {
		out = append(out, poolResource(project, pool))
	}
	return out, nil
}

// ListRelated lists a CA pool's authorities, issued certificates, or IAM
// bindings, and a Certificate Authority's CRLs.
func (p Pools) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	switch {
	case parent.Service == model.ServiceCAPool &&
		(facetID == model.FacetCertificateAuthorities || facetID == model.FacetCertificates || facetID == model.FacetPermissions):
	case parent.Service == model.ServiceCertificateAuthority && facetID == model.FacetCRLs:
	default:
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "caPools" {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("privateca: no API")
	}

	if parent.Service == model.ServiceCertificateAuthority {
		if len(n.Rest) != 2 || n.Rest[0] != "certificateAuthorities" || n.Rest[1] == "" {
			return nil, false, nil
		}
		// DEVOPS CAs do not support CRLs.
		if parent.Metadata["tier"] == "DEVOPS" {
			return nil, false, nil
		}
		crls, truncated, err := p.api.ListCRLs(ctx, n.Project, n.Location, n.Name, n.Rest[1], maxCRLs)
		if err != nil {
			return nil, false, err
		}
		sortCRLs(crls)
		out := make([]model.Resource, 0, len(crls))
		for _, crl := range crls {
			out = append(out, crlResource(parent, n.Location, crl))
		}
		return out, truncated, nil
	}

	if len(n.Rest) != 0 {
		return nil, false, nil
	}
	switch facetID {
	case model.FacetCertificateAuthorities:
		cas, err := p.api.ListAuthorities(ctx, n.Project, n.Location, n.Name)
		if err != nil {
			return nil, false, err
		}
		sortAuthorities(cas)
		now := p.now()
		out := make([]model.Resource, 0, len(cas))
		for _, ca := range cas {
			out = append(out, authorityResource(parent, n.Project, n.Location, ca, now))
		}
		return out, false, nil
	case model.FacetCertificates:
		// DEVOPS pools do not store or list issued certificates.
		if parent.Metadata["tier"] == "DEVOPS" {
			return nil, false, nil
		}
		certs, truncated, err := p.api.ListCertificates(ctx, n.Project, n.Location, n.Name, maxCertificates)
		if err != nil {
			return nil, false, err
		}
		sortCertificates(certs)
		now := p.now()
		out := make([]model.Resource, 0, len(certs))
		for _, c := range certs {
			out = append(out, certificateResource(parent, n.Project, n.Location, c, now))
		}
		return out, truncated, nil
	default: // model.FacetPermissions
		bindings, err := p.api.GetPoolIAMPolicy(ctx, n.Project, n.Location, n.Name)
		if err != nil {
			return nil, false, err
		}
		return bindingResources(parent, bindings), false, nil
	}
}

// Templates implements model.ResourceProvider for certificate templates,
// expanding a template to its IAM bindings.
type Templates struct{ api API }

func NewTemplates(api API) Templates { return Templates{api: api} }

func (p Templates) Service() model.ServiceType { return model.ServiceCertificateTemplate }

// ListPermissions is what List needs.
func (p Templates) ListPermissions(scopeID string) []string {
	return model.ProjectPermissions(scopeID, "privateca.certificateTemplates.list")
}

// List lists certificate templates across every Private CA location.
func (p Templates) List(ctx context.Context, scopeID string) ([]model.Resource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	project, ok := model.ProjectID(scopeID)
	if !ok {
		return nil, nil
	}
	if p.api == nil {
		return nil, errors.New("privateca: no API")
	}
	templates, err := p.api.ListTemplates(ctx, project)
	if err != nil {
		return nil, err
	}
	out := make([]model.Resource, 0, len(templates))
	for _, t := range templates {
		out = append(out, templateResource(project, t))
	}
	return out, nil
}

// ListRelated lists a certificate template's IAM bindings.
func (p Templates) ListRelated(ctx context.Context, parent model.Resource, facetID string) ([]model.Resource, bool, error) {
	if parent.Service != model.ServiceCertificateTemplate || facetID != model.FacetPermissions {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n, ok := model.ParseName(parent.ID)
	if !ok || n.Project == "" || n.Location == "" || n.Kind != "certificateTemplates" || len(n.Rest) != 0 {
		return nil, false, nil
	}
	if p.api == nil {
		return nil, false, errors.New("privateca: no API")
	}
	bindings, err := p.api.GetTemplateIAMPolicy(ctx, n.Project, n.Location, n.Name)
	if err != nil {
		return nil, false, err
	}
	return bindingResources(parent, bindings), false, nil
}

func bindingResources(parent model.Resource, bindings []Binding) []model.Resource {
	out := make([]model.Resource, 0, len(bindings))
	for _, b := range bindings {
		r := model.BindingResource(parent.ID, parent.ScopeID, b.Role, b.Members, b.Condition)
		r.ParentID = parent.ID
		r.FacetID = model.FacetPermissions
		out = append(out, r)
	}
	return out
}

func locatedID(project, location, kind, name string) string {
	return "projects/" + project + "/locations/" + url.PathEscape(location) + "/" + kind + "/" + url.PathEscape(name)
}

var (
	poolFields = []string{"tier", "publishCaCert", "publishCrl", "encoding", "allowedKeyTypes",
		"maxLifetime", "backdate", "issuanceModes", "identityPassthrough", "identityExpression",
		"passthroughExtensions", "kmsKey"}
	authorityFields = []string{"type", "tier", "subject", "issuer", "validFrom", "expires", "lifetime",
		"algorithm", "kmsKeyVersion", "serial", "sha256", "gcsBucket", "caCertUrl",
		"deleted", "purgeTime", "updated"}
	certificateFields = []string{"subject", "sans", "issuerCa", "template", "subjectMode",
		"validFrom", "expires", "lifetime", "serial", "sha256",
		"revocationReason", "revokedAt", "updated"}
	crlFields      = []string{"sequenceNumber", "revisionId", "revokedCount", "accessUrl", "updated"}
	templateFields = []string{"maxLifetime", "isCa", "keyUsage", "extendedKeyUsage",
		"identityPassthrough", "identityExpression", "passthroughExtensions", "updated"}
)

func poolResource(project string, p Pool) model.Resource {
	md := map[string]string{}
	set(md, "tier", p.Tier)
	md["publishCaCert"] = onOff(p.PublishCACert)
	if p.Tier == "ENTERPRISE" {
		md["publishCrl"] = onOff(p.PublishCRL)
	}
	set(md, "encoding", p.EncodingFormat)
	set(md, "allowedKeyTypes", strings.Join(p.AllowedKeyTypes, ", "))
	set(md, "maxLifetime", duration(p.MaxLifetime))
	switch {
	case p.BackdateDuration != "":
		set(md, "backdate", duration(p.BackdateDuration))
	case p.RequesterNotBefore:
		md["backdate"] = "requester-specified"
	}
	set(md, "issuanceModes", p.IssuanceModes)
	set(md, "identityPassthrough", p.IdentityPassthrough)
	set(md, "identityExpression", p.IdentityCEL)
	set(md, "passthroughExtensions", strings.Join(p.PassthroughExtensions, ", "))
	set(md, "kmsKey", p.KMSKey)

	facets := []model.Facet{{ID: model.FacetCertificateAuthorities, Label: "Certificate Authorities"}}
	if p.Tier == "ENTERPRISE" {
		facets = append(facets, model.Facet{ID: model.FacetCertificates, Label: "Certificates"})
	}
	facets = append(facets, model.Facet{ID: model.FacetPermissions, Label: "Permissions"})

	return model.Resource{
		ID:       locatedID(project, p.Location, "caPools", p.Name),
		Name:     p.Name,
		Service:  model.ServiceCAPool,
		ScopeID:  "projects/" + project,
		Location: p.Location,
		Labels:   maps.Clone(p.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, poolFields...),
		Badge:    p.Tier,
		Facets:   facets,
	}
}

func authorityResource(parent model.Resource, project, location string, ca Authority, now time.Time) model.Resource {
	md := map[string]string{}
	set(md, "type", authorityTypeWord(ca.Type))
	set(md, "tier", ca.Tier)
	set(md, "subject", ca.Subject)
	set(md, "issuer", shortCAName(project, location, ca.IssuerCA))
	set(md, "validFrom", display.Stamp(ca.NotBefore))
	set(md, "expires", display.Stamp(ca.NotAfter))
	set(md, "lifetime", duration(ca.Lifetime))
	set(md, "algorithm", ca.KeyAlgorithm)
	set(md, "kmsKeyVersion", ca.KMSKeyVersion)
	set(md, "serial", ca.Serial)
	set(md, "sha256", ca.SHA256)
	set(md, "gcsBucket", ca.GCSBucket)
	set(md, "caCertUrl", ca.CACertURL)
	set(md, "deleted", display.Stamp(ca.Deleted))
	set(md, "purgeTime", display.Stamp(ca.PurgeTime))
	set(md, "updated", display.Stamp(ca.Updated))

	status, badge := authorityStatus(ca, now)

	var lists []model.List
	if len(ca.SANs) > 0 {
		lists = append(lists, model.List{ID: "sans", Label: "Subject alternative names", Items: slices.Clone(ca.SANs)})
	}
	if len(ca.CRLURLs) > 0 {
		lists = append(lists, model.List{ID: "crlUrls", Label: "CRL URLs", Items: slices.Clone(ca.CRLURLs)})
	}

	var facets []model.Facet
	publishCRL := len(ca.CRLURLs) > 0 ||
		(parent.Metadata["publishCrl"] == "on" && ca.State != "AWAITING_USER_ACTIVATION" && ca.State != "DELETED")
	if ca.Tier == "ENTERPRISE" && publishCRL {
		facets = []model.Facet{{ID: model.FacetCRLs, Label: "Revocation Lists", Inline: true}}
	}

	var copies []model.Copy
	if len(ca.PEMCerts) > 0 {
		copies = append(copies, model.Copy{Title: "CA certificate (PEM)", Label: "CA certificate", Text: joinPEM(ca.PEMCerts[:1])})
		if len(ca.PEMCerts) > 1 {
			copies = append(copies, model.Copy{Title: "CA chain (PEM)", Label: "CA chain", Text: joinPEM(ca.PEMCerts)})
		}
	}

	return model.Resource{
		ID:       parent.ID + "/certificateAuthorities/" + url.PathEscape(ca.Name),
		Name:     ca.Name,
		Service:  model.ServiceCertificateAuthority,
		ScopeID:  parent.ScopeID,
		Location: location,
		Status:   status,
		State:    ca.State,
		Created:  ca.Created,
		Labels:   maps.Clone(ca.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, authorityFields...),
		Lists:    lists,
		Badge:    badge,
		Facets:   facets,
		ParentID: parent.ID,
		FacetID:  model.FacetCertificateAuthorities,
		Copies:   copies,
	}
}

func authorityStatus(ca Authority, now time.Time) (model.ResourceStatus, string) {
	status := model.StatusUnknown
	switch ca.State {
	case "ENABLED":
		status = model.StatusHealthy
	case "STAGED", "DISABLED", "AWAITING_USER_ACTIVATION", "DELETED":
		status = model.StatusWarning
	}
	if !ca.NotAfter.IsZero() && ca.State != "DELETED" {
		left := ca.NotAfter.Sub(now)
		switch {
		case left <= 0:
			return model.StatusError, "EXPIRED"
		case left <= expiryWarning:
			if status != model.StatusError {
				status = model.StatusWarning
			}
			return status, strconv.Itoa(int(left.Hours()/24)) + "d"
		}
	}
	if ca.State != "" && ca.State != "ENABLED" {
		return status, ca.State
	}
	switch ca.Type {
	case "SELF_SIGNED":
		return status, "ROOT"
	case "SUBORDINATE":
		return status, "SUB"
	default:
		return status, ""
	}
}

func certificateResource(parent model.Resource, project, location string, c IssuedCertificate, now time.Time) model.Resource {
	md := map[string]string{}
	set(md, "subject", c.Subject)
	set(md, "sans", strings.Join(c.SANs, ", "))
	set(md, "issuerCa", lastSeg(c.IssuerCA))
	set(md, "template", shortName(project, location, c.Template))
	if c.SubjectMode != "" && c.SubjectMode != "DEFAULT" {
		md["subjectMode"] = c.SubjectMode
	}
	set(md, "validFrom", display.Stamp(c.NotBefore))
	set(md, "expires", display.Stamp(c.NotAfter))
	set(md, "lifetime", duration(c.Lifetime))
	set(md, "serial", c.Serial)
	set(md, "sha256", c.SHA256)
	if c.Revoked {
		reason := c.RevocationReason
		if reason == "" {
			reason = "unspecified"
		}
		md["revocationReason"] = reason
		set(md, "revokedAt", display.Stamp(c.RevokedAt))
	}
	set(md, "updated", display.Stamp(c.Updated))

	state := "ACTIVE"
	if c.Revoked {
		state = "REVOKED"
	}
	status, badge := issuedCertStatus(c, now)

	var lists []model.List
	if len(c.SANs) > 0 {
		lists = append(lists, model.List{ID: "sans", Label: "Subject alternative names", Items: slices.Clone(c.SANs)})
	}

	var copies []model.Copy
	if c.PEMCert != "" {
		copies = append(copies, model.Copy{Title: "Certificate (PEM)", Label: "certificate", Text: joinPEM([]string{c.PEMCert})})
	}
	if len(c.PEMChain) > 0 {
		bundle := slices.Clone(c.PEMChain)
		if c.PEMCert != "" {
			bundle = append([]string{c.PEMCert}, bundle...)
		}
		copies = append(copies, model.Copy{Title: "Certificate chain (PEM)", Label: "certificate chain", Text: joinPEM(bundle)})
	}

	return model.Resource{
		ID:       parent.ID + "/certificates/" + url.PathEscape(c.Name),
		Name:     c.Name,
		Service:  model.ServiceCACertificate,
		ScopeID:  parent.ScopeID,
		Location: location,
		Status:   status,
		State:    state,
		Created:  c.Created,
		Labels:   maps.Clone(c.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, certificateFields...),
		Lists:    lists,
		Badge:    badge,
		ParentID: parent.ID,
		FacetID:  model.FacetCertificates,
		Copies:   copies,
	}
}

func issuedCertStatus(c IssuedCertificate, now time.Time) (model.ResourceStatus, string) {
	if c.Revoked {
		return model.StatusError, "REVOKED"
	}
	if !c.NotAfter.IsZero() {
		left := c.NotAfter.Sub(now)
		switch {
		case left <= 0:
			return model.StatusError, "EXPIRED"
		case left <= expiryWarning:
			return model.StatusWarning, strconv.Itoa(int(left.Hours()/24)) + "d"
		}
	}
	return model.StatusHealthy, ""
}

func crlResource(parent model.Resource, location string, crl CRL) model.Resource {
	md := map[string]string{
		"sequenceNumber": strconv.FormatInt(crl.SequenceNumber, 10),
		"revokedCount":   strconv.Itoa(len(crl.Revoked)),
	}
	set(md, "revisionId", crl.RevisionID)
	set(md, "accessUrl", crl.AccessURL)
	set(md, "updated", display.Stamp(crl.Updated))

	status := model.StatusUnknown
	if crl.State == "ACTIVE" {
		status = model.StatusHealthy
	}

	var lists []model.List
	if len(crl.Revoked) > 0 {
		items := make([]string, 0, len(crl.Revoked))
		for _, r := range crl.Revoked {
			line := r.Serial
			if line == "" {
				line = lastSeg(r.Certificate)
			}
			if r.Reason != "" {
				line += "  " + r.Reason
			}
			if cert := lastSeg(r.Certificate); cert != "" && r.Serial != "" {
				line += "  (" + cert + ")"
			}
			items = append(items, line)
		}
		lists = append(lists, model.List{ID: "revoked", Label: "Revoked certificates", Items: items})
	}

	var copies []model.Copy
	if crl.PEMCRL != "" {
		copies = append(copies, model.Copy{Title: "CRL (PEM)", Label: "CRL", Text: joinPEM([]string{crl.PEMCRL})})
	}

	return model.Resource{
		ID:       parent.ID + "/certificateRevocationLists/" + url.PathEscape(crl.Name),
		Name:     crl.Name,
		Service:  model.ServiceCertificateRevocationList,
		ScopeID:  parent.ScopeID,
		Location: location,
		Status:   status,
		State:    crl.State,
		Created:  crl.Created,
		Labels:   maps.Clone(crl.Labels),
		Metadata: md,
		Fields:   model.FieldsOf(md, crlFields...),
		Lists:    lists,
		Badge:    crl.State,
		ParentID: parent.ID,
		FacetID:  model.FacetCRLs,
		Copies:   copies,
	}
}

func templateResource(project string, t Template) model.Resource {
	md := map[string]string{}
	set(md, "maxLifetime", duration(t.MaxLifetime))
	if t.IsCA {
		if t.MaxIssuerPathLength > 0 {
			md["isCa"] = fmt.Sprintf("yes (max path %d)", t.MaxIssuerPathLength)
		} else {
			md["isCa"] = "yes"
		}
	}
	set(md, "keyUsage", strings.Join(t.KeyUsages, ", "))
	set(md, "extendedKeyUsage", strings.Join(t.ExtendedKeyUsages, ", "))
	set(md, "identityPassthrough", t.IdentityPassthrough)
	set(md, "identityExpression", t.IdentityCEL)
	set(md, "passthroughExtensions", strings.Join(t.PassthroughExtensions, ", "))
	set(md, "updated", display.Stamp(t.Updated))

	return model.Resource{
		ID:          locatedID(project, t.Location, "certificateTemplates", t.Name),
		Name:        t.Name,
		Service:     model.ServiceCertificateTemplate,
		ScopeID:     "projects/" + project,
		Location:    t.Location,
		Description: t.Description,
		Created:     t.Created,
		Labels:      maps.Clone(t.Labels),
		Metadata:    md,
		Fields:      model.FieldsOf(md, templateFields...),
		Facets:      []model.Facet{{ID: model.FacetPermissions, Label: "Permissions"}},
	}
}

func authorityTypeWord(t string) string {
	switch t {
	case "SELF_SIGNED":
		return "root (self-signed)"
	case "SUBORDINATE":
		return "subordinate"
	default:
		return t
	}
}

// sortAuthorities orders enabled CAs first, then staged, awaiting activation,
// disabled, and deleted, and alphabetically within each state.
func sortAuthorities(cas []Authority) {
	rank := func(state string) int {
		switch state {
		case "ENABLED":
			return 0
		case "STAGED":
			return 1
		case "AWAITING_USER_ACTIVATION":
			return 2
		case "DISABLED":
			return 3
		case "DELETED":
			return 4
		default:
			return 5
		}
	}
	slices.SortStableFunc(cas, func(a, b Authority) int {
		if c := cmp.Compare(rank(a.State), rank(b.State)); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

// sortCertificates orders newest certificates first, then by name.
func sortCertificates(certs []IssuedCertificate) {
	slices.SortStableFunc(certs, func(a, b IssuedCertificate) int {
		if !a.Created.Equal(b.Created) {
			return b.Created.Compare(a.Created)
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// sortCRLs orders active CRLs ahead of superseded ones, then by descending
// sequence number.
func sortCRLs(crls []CRL) {
	slices.SortStableFunc(crls, func(a, b CRL) int {
		aActive, bActive := a.State == "ACTIVE", b.State == "ACTIVE"
		if aActive != bActive {
			if aActive {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.SequenceNumber, a.SequenceNumber); c != 0 {
			return c
		}
		return b.Created.Compare(a.Created)
	})
}

// joinPEM concatenates PEM blocks, each ending in a single newline.
func joinPEM(pems []string) string {
	var b strings.Builder
	for _, p := range pems {
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// duration turns the API's "7776000s" into "90 days" (or "12h0m0s" when not
// whole days); other strings stay as they are.
func duration(s string) string {
	if s == "" {
		return ""
	}
	secs, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
	if err != nil || !strings.HasSuffix(s, "s") {
		return s
	}
	d := time.Duration(secs * float64(time.Second))
	if d > 0 && d%(24*time.Hour) == 0 {
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return d.String()
}

// shortName drops the project/location prefix from a sibling resource's full
// name when it belongs to the same project and location.
func shortName(project, location, name string) string {
	n, ok := model.ParseName(name)
	if !ok || n.Project != project || n.Location != location || len(n.Rest) != 0 {
		return name
	}
	return n.Name
}

// shortCAName shortens projects/p/locations/l/caPools/pool/certificateAuthorities/ca
// to pool/ca when in the same project and location.
func shortCAName(project, location, name string) string {
	n, ok := model.ParseName(name)
	if !ok || n.Project != project || n.Location != location || n.Kind != "caPools" ||
		len(n.Rest) != 2 || n.Rest[0] != "certificateAuthorities" {
		return name
	}
	return n.Name + "/" + n.Rest[1]
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func set(md map[string]string, k, v string) {
	if v != "" {
		md[k] = v
	}
}

var (
	_ model.ResourceProvider   = Pools{}
	_ model.PermissionDeclarer = Pools{}
	_ model.RelatedLister      = Pools{}
	_ model.ResourceProvider   = Templates{}
	_ model.PermissionDeclarer = Templates{}
	_ model.RelatedLister      = Templates{}
)
