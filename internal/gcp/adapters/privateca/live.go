package privateca

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/option"
	cas "google.golang.org/api/privateca/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct{ svc *cas.Service }

// NewLazy defers client construction to the first call, so a slow or failing
// constructor cannot block startup; the failure surfaces as that service's
// ProviderError instead. Share the result across Pools and Templates so the
// client is built once.
func NewLazy(opts ...option.ClientOption) API {
	return lazyAPI{l: lazy.New(func(ctx context.Context) (*cas.Service, error) {
		return cas.NewService(ctx, opts...)
	})}
}

type lazyAPI struct{ l *lazy.Value[*cas.Service] }

func (a lazyAPI) ListPools(ctx context.Context, project string) ([]Pool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListPools(ctx, project)
}

func (a lazyAPI) ListAuthorities(ctx context.Context, project, location, pool string) ([]Authority, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListAuthorities(ctx, project, location, pool)
}

func (a lazyAPI) ListCertificates(ctx context.Context, project, location, pool string, limit int) ([]IssuedCertificate, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListCertificates(ctx, project, location, pool, limit)
}

func (a lazyAPI) ListCRLs(ctx context.Context, project, location, pool, ca string, limit int) ([]CRL, bool, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, false, err
	}
	return liveAPI{svc: svc}.ListCRLs(ctx, project, location, pool, ca, limit)
}

func (a lazyAPI) ListTemplates(ctx context.Context, project string) ([]Template, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListTemplates(ctx, project)
}

func (a lazyAPI) GetPoolIAMPolicy(ctx context.Context, project, location, pool string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetPoolIAMPolicy(ctx, project, location, pool)
}

func (a lazyAPI) GetTemplateIAMPolicy(ctx context.Context, project, location, template string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetTemplateIAMPolicy(ctx, project, location, template)
}

func (a lazyAPI) GetCRLIAMPolicy(ctx context.Context, project, location, pool, ca, crl string) ([]Binding, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.GetCRLIAMPolicy(ctx, project, location, pool, ca, crl)
}

var (
	_ API = lazyAPI{}
	_ API = liveAPI{}
)

// errStop ends paging once a facet limit is reached.
var errStop = errors.New("stop")

// ListPools uses the "-" location wildcard so one paginated call covers every
// region. Unreachable regions are logged and ignored.
func (a liveAPI) ListPools(ctx context.Context, project string) ([]Pool, error) {
	var out []Pool
	var missed unreachable
	err := a.svc.Projects.Locations.CaPools.List("projects/"+project+"/locations/-").Pages(ctx, func(resp *cas.ListCaPoolsResponse) error {
		missed.add(resp.Unreachable)
		for _, p := range resp.CaPools {
			out = append(out, fromPool(p))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	missed.log("CA pool", project)
	return out, nil
}

func (a liveAPI) ListAuthorities(ctx context.Context, project, location, pool string) ([]Authority, error) {
	var out []Authority
	parent := poolName(project, location, pool)
	err := a.svc.Projects.Locations.CaPools.CertificateAuthorities.List(parent).Pages(ctx, func(resp *cas.ListCertificateAuthoritiesResponse) error {
		for _, ca := range resp.CertificateAuthorities {
			out = append(out, fromAuthority(ca))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a liveAPI) ListCertificates(ctx context.Context, project, location, pool string, limit int) ([]IssuedCertificate, bool, error) {
	var out []IssuedCertificate
	truncated := false
	parent := poolName(project, location, pool)
	err := a.svc.Projects.Locations.CaPools.Certificates.List(parent).Pages(ctx, func(resp *cas.ListCertificatesResponse) error {
		for _, c := range resp.Certificates {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromCertificate(c))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

func (a liveAPI) ListCRLs(ctx context.Context, project, location, pool, ca string, limit int) ([]CRL, bool, error) {
	var out []CRL
	truncated := false
	parent := poolName(project, location, pool) + "/certificateAuthorities/" + ca
	err := a.svc.Projects.Locations.CaPools.CertificateAuthorities.CertificateRevocationLists.List(parent).Pages(ctx, func(resp *cas.ListCertificateRevocationListsResponse) error {
		for _, crl := range resp.CertificateRevocationLists {
			if len(out) == limit {
				truncated = true
				return errStop
			}
			out = append(out, fromCRL(crl))
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, false, err
	}
	return out, truncated, nil
}

// ListTemplates uses the "-" location wildcard across every region.
func (a liveAPI) ListTemplates(ctx context.Context, project string) ([]Template, error) {
	var out []Template
	var missed unreachable
	err := a.svc.Projects.Locations.CertificateTemplates.List("projects/"+project+"/locations/-").Pages(ctx, func(resp *cas.ListCertificateTemplatesResponse) error {
		missed.add(resp.Unreachable)
		for _, t := range resp.CertificateTemplates {
			out = append(out, fromTemplate(t))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	missed.log("certificate template", project)
	return out, nil
}

func (a liveAPI) GetPoolIAMPolicy(ctx context.Context, project, location, pool string) ([]Binding, error) {
	policy, err := a.svc.Projects.Locations.CaPools.GetIamPolicy(poolName(project, location, pool)).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func (a liveAPI) GetTemplateIAMPolicy(ctx context.Context, project, location, template string) ([]Binding, error) {
	name := "projects/" + project + "/locations/" + location + "/certificateTemplates/" + template
	policy, err := a.svc.Projects.Locations.CertificateTemplates.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func (a liveAPI) GetCRLIAMPolicy(ctx context.Context, project, location, pool, ca, crl string) ([]Binding, error) {
	name := poolName(project, location, pool) + "/certificateAuthorities/" + ca + "/certificateRevocationLists/" + crl
	policy, err := a.svc.Projects.Locations.CaPools.CertificateAuthorities.CertificateRevocationLists.GetIamPolicy(name).OptionsRequestedPolicyVersion(3).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return fromBindings(policy.Bindings), nil
}

func poolName(project, location, pool string) string {
	return "projects/" + project + "/locations/" + location + "/caPools/" + pool
}

// unreachable collects the locations a wildcard list's pages could not reach,
// without repeats.
type unreachable []string

func (u *unreachable) add(locs []string) {
	for _, l := range locs {
		if !slices.Contains(*u, l) {
			*u = append(*u, l)
		}
	}
}

func (u unreachable) log(kind, project string) {
	if len(u) > 0 {
		slog.Debug("privateca: some "+kind+" locations did not respond", "project", project, "locations", []string(u))
	}
}

func fromPool(p *cas.CaPool) Pool {
	tier := unspecified(p.Tier)
	out := Pool{
		Name:          lastSeg(p.Name),
		Location:      locationOf(p.Name),
		Tier:          tier,
		PublishCACert: true,
		PublishCRL:    tier == "ENTERPRISE",
		Labels:        maps.Clone(p.Labels),
	}
	if po := p.PublishingOptions; po != nil {
		out.PublishCACert = po.PublishCaCert
		out.PublishCRL = po.PublishCrl
		out.EncodingFormat = unspecified(po.EncodingFormat)
	}
	if es := p.EncryptionSpec; es != nil {
		out.KMSKey = es.CloudKmsKey
	}
	if ip := p.IssuancePolicy; ip != nil {
		out.MaxLifetime = ip.MaximumLifetime
		out.BackdateDuration = ip.BackdateDuration
		out.RequesterNotBefore = ip.AllowRequesterSpecifiedNotBeforeTime
		for _, kt := range ip.AllowedKeyTypes {
			if s := formatKeyType(kt); s != "" {
				out.AllowedKeyTypes = append(out.AllowedKeyTypes, s)
			}
		}
		if im := ip.AllowedIssuanceModes; im != nil {
			out.IssuanceModes = formatIssuanceModes(im)
		}
		if ic := ip.IdentityConstraints; ic != nil {
			out.IdentityPassthrough = formatIdentityPassthrough(ic)
			out.IdentityCEL = exprTitle(ic.CelExpression)
		}
		out.PassthroughExtensions = formatExtensionConstraints(ip.PassthroughExtensions)
	}
	return out
}

func fromAuthority(ca *cas.CertificateAuthority) Authority {
	out := Authority{
		Name:      lastSeg(ca.Name),
		Type:      unspecified(ca.Type),
		State:     unspecified(ca.State),
		Tier:      unspecified(ca.Tier),
		Lifetime:  ca.Lifetime,
		GCSBucket: ca.GcsBucket,
		PEMCerts:  slices.Clone(ca.PemCaCertificates),
		Created:   parseTime(ca.CreateTime),
		Updated:   parseTime(ca.UpdateTime),
		Deleted:   parseTime(ca.DeleteTime),
		PurgeTime: parseTime(ca.ExpireTime),
		Labels:    maps.Clone(ca.Labels),
	}
	if ks := ca.KeySpec; ks != nil {
		out.KeyAlgorithm = unspecified(ks.Algorithm)
		out.KMSKeyVersion = ks.CloudKmsKeyVersion
	}
	if au := ca.AccessUrls; au != nil {
		out.CACertURL = au.CaCertificateAccessUrl
		out.CRLURLs = slices.Clone(au.CrlAccessUrls)
	}
	if sc := ca.SubordinateConfig; sc != nil {
		out.IssuerCA = sc.CertificateAuthority
	}
	if len(ca.CaCertificateDescriptions) > 0 {
		fillCertDescription(ca.CaCertificateDescriptions[0], &out.Subject, &out.SANs, &out.NotBefore, &out.NotAfter, &out.Serial, &out.SHA256)
	} else if ca.Config != nil && ca.Config.SubjectConfig != nil {
		sc := ca.Config.SubjectConfig
		out.Subject = formatSubject(sc.Subject)
		out.SANs = formatSANs(sc.SubjectAltName)
	}
	return out
}

func fromCertificate(c *cas.Certificate) IssuedCertificate {
	out := IssuedCertificate{
		Name:        lastSeg(c.Name),
		IssuerCA:    c.IssuerCertificateAuthority,
		Template:    c.CertificateTemplate,
		SubjectMode: unspecified(c.SubjectMode),
		Lifetime:    c.Lifetime,
		PEMCert:     c.PemCertificate,
		PEMChain:    slices.Clone(c.PemCertificateChain),
		Created:     parseTime(c.CreateTime),
		Updated:     parseTime(c.UpdateTime),
		Labels:      maps.Clone(c.Labels),
	}
	if c.CertificateDescription != nil {
		fillCertDescription(c.CertificateDescription, &out.Subject, &out.SANs, &out.NotBefore, &out.NotAfter, &out.Serial, &out.SHA256)
	} else if c.Config != nil && c.Config.SubjectConfig != nil {
		sc := c.Config.SubjectConfig
		out.Subject = formatSubject(sc.Subject)
		out.SANs = formatSANs(sc.SubjectAltName)
	}
	if rd := c.RevocationDetails; rd != nil {
		out.Revoked = true
		out.RevocationReason = unspecified(rd.RevocationState)
		out.RevokedAt = parseTime(rd.RevocationTime)
	}
	return out
}

func fromCRL(crl *cas.CertificateRevocationList) CRL {
	out := CRL{
		Name:           lastSeg(crl.Name),
		State:          unspecified(crl.State),
		SequenceNumber: crl.SequenceNumber,
		RevisionID:     crl.RevisionId,
		AccessURL:      crl.AccessUrl,
		PEMCRL:         crl.PemCrl,
		Created:        parseTime(crl.CreateTime),
		Updated:        parseTime(crl.UpdateTime),
		Labels:         maps.Clone(crl.Labels),
	}
	for _, r := range crl.RevokedCertificates {
		out.Revoked = append(out.Revoked, RevokedCert{
			Certificate: r.Certificate,
			Serial:      r.HexSerialNumber,
			Reason:      unspecified(r.RevocationReason),
		})
	}
	return out
}

func fromTemplate(t *cas.CertificateTemplate) Template {
	out := Template{
		Name:                  lastSeg(t.Name),
		Location:              locationOf(t.Name),
		Description:           t.Description,
		MaxLifetime:           t.MaximumLifetime,
		PassthroughExtensions: formatExtensionConstraints(t.PassthroughExtensions),
		Created:               parseTime(t.CreateTime),
		Updated:               parseTime(t.UpdateTime),
		Labels:                maps.Clone(t.Labels),
	}
	if pv := t.PredefinedValues; pv != nil {
		if pv.CaOptions != nil && pv.CaOptions.IsCa {
			out.IsCA = true
			out.MaxIssuerPathLength = pv.CaOptions.MaxIssuerPathLength
		}
		out.KeyUsages, out.ExtendedKeyUsages = formatKeyUsages(pv.KeyUsage)
	}
	if ic := t.IdentityConstraints; ic != nil {
		out.IdentityPassthrough = formatIdentityPassthrough(ic)
		out.IdentityCEL = exprTitle(ic.CelExpression)
	}
	return out
}

func fromBindings(bindings []*cas.Binding) []Binding {
	out := make([]Binding, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, Binding{
			Role:      b.Role,
			Members:   slices.Clone(b.Members),
			Condition: exprTitle(b.Condition),
		})
	}
	return out
}

func fillCertDescription(d *cas.CertificateDescription, subject *string, sans *[]string, notBefore, notAfter *time.Time, serial, sha256 *string) {
	if d == nil {
		return
	}
	if sd := d.SubjectDescription; sd != nil {
		*subject = formatSubject(sd.Subject)
		*sans = formatSANs(sd.SubjectAltName)
		*notBefore = parseTime(sd.NotBeforeTime)
		*notAfter = parseTime(sd.NotAfterTime)
		*serial = sd.HexSerialNumber
	}
	if cf := d.CertFingerprint; cf != nil {
		*sha256 = cf.Sha256Hash
	}
}

func formatSubject(s *cas.Subject) string {
	if s == nil {
		return ""
	}
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("CN", s.CommonName)
	add("OU", s.OrganizationalUnit)
	add("O", s.Organization)
	add("L", s.Locality)
	add("ST", s.Province)
	add("C", s.CountryCode)
	return strings.Join(parts, ", ")
}

func formatSANs(s *cas.SubjectAltNames) []string {
	if s == nil {
		return nil
	}
	var out []string
	out = append(out, s.DnsNames...)
	for _, ip := range s.IpAddresses {
		out = append(out, "ip:"+ip)
	}
	out = append(out, s.Uris...)
	for _, email := range s.EmailAddresses {
		out = append(out, "email:"+email)
	}
	return out
}

func formatKeyType(kt *cas.AllowedKeyType) string {
	if kt == nil {
		return ""
	}
	if rsa := kt.Rsa; rsa != nil {
		switch {
		case rsa.MinModulusSize > 0 && rsa.MaxModulusSize > 0 && rsa.MinModulusSize == rsa.MaxModulusSize:
			return fmt.Sprintf("RSA %d", rsa.MinModulusSize)
		case rsa.MinModulusSize > 0 && rsa.MaxModulusSize > 0:
			return fmt.Sprintf("RSA %d–%d", rsa.MinModulusSize, rsa.MaxModulusSize)
		case rsa.MinModulusSize > 0:
			return fmt.Sprintf("RSA ≥%d", rsa.MinModulusSize)
		case rsa.MaxModulusSize > 0:
			return fmt.Sprintf("RSA ≤%d", rsa.MaxModulusSize)
		default:
			return "RSA"
		}
	}
	if ec := kt.EllipticCurve; ec != nil {
		if alg := unspecified(ec.SignatureAlgorithm); alg != "" {
			return alg
		}
		return "EC"
	}
	return ""
}

func formatIssuanceModes(m *cas.IssuanceModes) string {
	switch {
	case m.AllowCsrBasedIssuance && m.AllowConfigBasedIssuance:
		return "CSR, config"
	case m.AllowCsrBasedIssuance:
		return "CSR only"
	case m.AllowConfigBasedIssuance:
		return "config only"
	default:
		return "none"
	}
}

func formatIdentityPassthrough(ic *cas.CertificateIdentityConstraints) string {
	switch {
	case ic.AllowSubjectPassthrough && ic.AllowSubjectAltNamesPassthrough:
		return "subject, SANs"
	case ic.AllowSubjectAltNamesPassthrough:
		return "SANs"
	case ic.AllowSubjectPassthrough:
		return "subject"
	default:
		return "none"
	}
}

func formatExtensionConstraints(ec *cas.CertificateExtensionConstraints) []string {
	if ec == nil {
		return nil
	}
	var out []string
	for _, k := range ec.KnownExtensions {
		if s := unspecified(k); s != "" {
			out = append(out, s)
		}
	}
	for _, oid := range ec.AdditionalExtensions {
		if s := formatOID(oid); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func formatKeyUsages(ku *cas.KeyUsage) (base, ext []string) {
	if ku == nil {
		return nil, nil
	}
	if b := ku.BaseKeyUsage; b != nil {
		add := func(ok bool, name string) {
			if ok {
				base = append(base, name)
			}
		}
		add(b.DigitalSignature, "digitalSignature")
		add(b.ContentCommitment, "contentCommitment")
		add(b.KeyEncipherment, "keyEncipherment")
		add(b.DataEncipherment, "dataEncipherment")
		add(b.KeyAgreement, "keyAgreement")
		add(b.CertSign, "certSign")
		add(b.CrlSign, "crlSign")
		add(b.EncipherOnly, "encipherOnly")
		add(b.DecipherOnly, "decipherOnly")
	}
	if e := ku.ExtendedKeyUsage; e != nil {
		add := func(ok bool, name string) {
			if ok {
				ext = append(ext, name)
			}
		}
		add(e.ServerAuth, "serverAuth")
		add(e.ClientAuth, "clientAuth")
		add(e.CodeSigning, "codeSigning")
		add(e.EmailProtection, "emailProtection")
		add(e.TimeStamping, "timeStamping")
		add(e.OcspSigning, "ocspSigning")
	}
	for _, oid := range ku.UnknownExtendedKeyUsages {
		if s := formatOID(oid); s != "" {
			ext = append(ext, s)
		}
	}
	return base, ext
}

func formatOID(oid *cas.ObjectId) string {
	if oid == nil || len(oid.ObjectIdPath) == 0 {
		return ""
	}
	parts := make([]string, len(oid.ObjectIdPath))
	for i, n := range oid.ObjectIdPath {
		parts[i] = strconv.FormatInt(n, 10)
	}
	return strings.Join(parts, ".")
}

func exprTitle(e *cas.Expr) string {
	if e == nil {
		return ""
	}
	if e.Title != "" {
		return e.Title
	}
	return e.Expression
}

func unspecified(s string) string {
	if strings.HasSuffix(s, "_UNSPECIFIED") {
		return ""
	}
	return s
}

func locationOf(name string) string {
	segs := strings.Split(name, "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "locations" {
			return segs[i+1]
		}
	}
	return ""
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
