package model

import (
	"slices"
	"testing"
)

func TestGoogleServiceAccount(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"deployer@p.iam.gserviceaccount.com":                         false,
		"terraform@seed-project.iam.gserviceaccount.com":             false,
		"service-account@p.iam.gserviceaccount.com":                  false,
		"service-1a@p.iam.gserviceaccount.com":                       false,
		"user@example.com":                                           false,
		"":                                                           false,
		"@developer.gserviceaccount.com":                             false,
		"123-compute@developer.gserviceaccount.com":                  true,
		"p@appspot.gserviceaccount.com":                              true,
		"123@cloudservices.gserviceaccount.com":                      true,
		"123@cloudbuild.gserviceaccount.com":                         true,
		"service-123@gcp-sa-logging.iam.gserviceaccount.com":         true,
		"o123-111@gcp-sa-logging.iam.gserviceaccount.com":            true,
		"service-123@container-engine-robot.iam.gserviceaccount.com": true,
		"service-123@compute-system.iam.gserviceaccount.com":         true,
		"service-org-123@gcp-sa-x.iam.gserviceaccount.com":           true,
		"service-folder-123@p.iam.gserviceaccount.com":               true,
		"Service-123@Serverless-Robot-Prod.iam.gserviceaccount.com":  true,
	}
	for email, want := range cases {
		if got := GoogleServiceAccount(email); got != want {
			t.Errorf("%q: %v, want %v", email, got, want)
		}
	}
}

func TestWithoutGoogleServiceAccounts(t *testing.T) {
	t.Parallel()
	sa := func(email string) Resource {
		return Resource{ID: email, Service: ServiceServiceAccount, Metadata: map[string]string{MetaEmail: email}}
	}
	mine := sa("deployer@p.iam.gserviceaccount.com")
	compute := sa("123-compute@developer.gserviceaccount.com")
	agent := sa("service-123@gcp-sa-logging.iam.gserviceaccount.com")
	// A sink's writer identity names an agent but is not a service account.
	sink := Resource{ID: "sink", Service: ServiceLogSink, Metadata: map[string]string{MetaEmail: agent.ID}}

	cases := []struct {
		name string
		in   []Resource
		want []string
	}{
		{"none", nil, nil},
		{"all mine", []Resource{mine, sink}, []string{mine.ID, "sink"}},
		{"mixed", []Resource{compute, mine, agent, sink}, []string{mine.ID, "sink"}},
		{"all google", []Resource{compute, agent}, nil},
	}
	for _, tc := range cases {
		var ids []string
		for _, r := range WithoutGoogleServiceAccounts(tc.in) {
			ids = append(ids, r.ID)
		}
		if !slices.Equal(ids, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, ids, tc.want)
		}
	}
	in := []Resource{compute, mine}
	_ = WithoutGoogleServiceAccounts(in)
	if in[0].ID != compute.ID {
		t.Error("input modified")
	}
}
