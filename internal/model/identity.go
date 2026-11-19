package model

// Identity is the Google-free view of who the process will act as.
// UI and app must not import Google client libraries; they consume this struct.
type Identity struct {
	// Account is the principal shown in the header (email, or a short label).
	Account string
	// ADCProject is the project id from ADC, if any. It may differ from the
	// scope being browsed; the TUI must not silently switch scopes to it.
	ADCProject string
	// Impersonate is the target service account, if requested.
	Impersonate string
	// Source is "adc", "impersonate", or "none".
	Source string
	// Err is a credentials or live catalog problem. The TUI still starts,
	// with no catalog; show it as a banner.
	Err error
	// Notice is an informational line, not an error: credentials are fine
	// but something about the session deserves a mention, such as having
	// no project to browse.
	Notice string
}

// NoticeNoProject is the Notice when ADC is usable but no project was set.
const NoticeNoProject = "no project set; use --project or --initial-resource (or --demo for demo data)"

const (
	IdentityNone        = "none"
	IdentityADC         = "adc"
	IdentityImpersonate = "impersonate"
)

// DisplayAccount is the header/status label.
func (id Identity) DisplayAccount() string {
	if acct := cleanLine(id.Account); acct != "" {
		return acct
	}
	return "not signed in"
}

// Banner is a persistent auth warning, or empty when credentials look usable.
func (id Identity) Banner() string {
	if id.Err == nil {
		return ""
	}
	msg := cleanLine(id.Err.Error())
	if msg == "" {
		msg = "credentials unavailable"
	}
	return "auth: " + msg
}

// Account labels parseADC uses for credentials that are not a user's
// gcloud sign-in.
const (
	AccountMetadata         = "compute metadata"
	AccountWorkloadIdentity = "workload identity"
)

// RejectedBanner is the persistent warning once Google rejects the live
// credentials mid-session (expired or revoked sign-in). msg is the
// classified reason. Refresh (r) reads credentials again, so the fix is to
// repair them and press r. The gcloud command only helps a user sign-in.
func (id Identity) RejectedBanner(msg string) string {
	msg = cleanLine(msg)
	if msg == "" {
		msg = "credentials rejected"
	}
	switch {
	case id.Source == IdentityImpersonate && id.Impersonate != "":
		return "auth: " + msg + " impersonating " + cleanLine(id.Impersonate) + "; check the source credentials, then press r"
	case id.Account == AccountMetadata || id.Account == AccountWorkloadIdentity:
		return "auth: " + msg + " (" + id.Account + "); check the credentials seiner runs with, then press r"
	}
	return "auth: " + msg + "; run gcloud auth application-default login, then press r"
}
