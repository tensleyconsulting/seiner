package model

import (
	"slices"
	"testing"
)

func TestGroupByServiceOmitsEmpty(t *testing.T) {
	t.Parallel()
	items := []Resource{
		{ID: "b1", Name: "bucket", Service: ServiceCloudStorage},
		{ID: "b2", Name: "other", Service: ServiceCloudStorage},
		{ID: "iam", Name: "owners", Service: ServiceIAMBinding},
	}
	got := GroupByService(items)
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got[0].Service != ServiceCloudStorage || len(got[0].Items) != 2 {
		t.Fatalf("gcs %+v", got[0])
	}
	if got[1].Service != ServiceIAMBinding || got[1].Items[0].Name != "owners" {
		t.Fatalf("iam %+v", got[1])
	}
}

func TestGroupByServiceSortsItemsByName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"api order ignored", []string{"zeta", "alpha", "mid"}, []string{"alpha", "mid", "zeta"}},
		{"case-insensitive", []string{"Beta", "alpha", "Gamma"}, []string{"alpha", "Beta", "Gamma"}},
		{"stable on equal names", []string{"b", "a", "B"}, []string{"a", "b", "B"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var items []Resource
			for _, n := range tt.in {
				items = append(items, Resource{ID: n, Name: n, Service: ServiceCloudStorage})
			}
			got := GroupByService(items)
			if len(got) != 1 {
				t.Fatalf("groups %+v", got)
			}
			var names []string
			for _, r := range got[0].Items {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tt.want) {
				t.Fatalf("order %v, want %v", names, tt.want)
			}
		})
	}
}

func TestGroupByServiceOrdersUnknownServices(t *testing.T) {
	t.Parallel()
	items := []Resource{
		{ID: "z", Name: "z", Service: ServiceType("zz-unknown")},
		{ID: "a", Name: "a", Service: ServiceType("aa-unknown")},
		{ID: "b", Name: "bucket", Service: ServiceCloudStorage},
	}
	for range 20 {
		got := GroupByService(items)
		if len(got) != 3 || got[0].Service != ServiceCloudStorage || got[1].Service != "aa-unknown" || got[2].Service != "zz-unknown" {
			t.Fatalf("%+v", got)
		}
	}
}

func TestGroupByServiceSkipsCRM(t *testing.T) {
	t.Parallel()
	items := []Resource{
		HierarchyNode{ID: "p", Name: "p", Kind: KindProject}.Resource(),
		{ID: "b", Name: "bucket", Service: ServiceCloudStorage},
	}
	got := GroupByService(items)
	if len(got) != 1 || got[0].Service != ServiceCloudStorage {
		t.Fatalf("%+v", got)
	}
}

func TestCollectionResource(t *testing.T) {
	t.Parallel()
	owner := Resource{ID: "demo-project", Name: "demo-project", Service: ServiceProject, ScopeID: "demo-project"}
	c := CollectionResource(owner, ServiceCloudStorage, 2)
	if !c.IsCollection() || c.Name != "Cloud Storage" {
		t.Fatalf("%+v", c)
	}
	e := EnabledServicesResource(owner)
	if !e.IsEnabledGroup() || e.Name != "Enabled Services" {
		t.Fatalf("%+v", e)
	}
}

func TestGroupByProductNests(t *testing.T) {
	t.Parallel()
	items := []Resource{
		{ID: "lb", Name: "web-lb", Service: ServiceLoadBalancer},
		{ID: "sa", Name: "api", Service: ServiceServiceAttachment},
		{ID: "ep", Name: "to-api", Service: ServicePSCEndpoint},
		{ID: "fr", Name: "web-https", Service: ServiceForwardingRule},
	}
	got := GroupByProduct(GroupByService(items))
	if len(got) != 1 || got[0].Product != ServiceNetworkServices {
		t.Fatalf("sections %+v", got)
	}
	ns := got[0]
	var own []ServiceType
	for _, g := range ns.Groups {
		own = append(own, g.Service)
	}
	if want := []ServiceType{ServiceLoadBalancer, ServiceForwardingRule}; !slices.Equal(own, want) {
		t.Fatalf("network services groups %v, want %v", own, want)
	}
	if len(ns.Subs) != 1 || ns.Subs[0].Product != ServicePSC || len(ns.Subs[0].Groups) != 2 {
		t.Fatalf("subs %+v", ns.Subs)
	}
	if ns.Items() != 4 {
		t.Fatalf("items %d", ns.Items())
	}
	for _, tc := range []struct {
		svc, product ServiceType
		want         bool
	}{
		{ServiceServiceAttachment, ServicePSC, true},
		{ServiceServiceAttachment, ServiceNetworkServices, true},
		{ServiceForwardingRule, ServiceNetworkServices, true},
		{ServiceForwardingRule, ServicePSC, false},
		{ServiceSSLPolicy, ServiceNetworkServices, false},
		{ServiceSSLPolicy, ServiceNetworkSecurity, true},
		{ServiceCloudArmor, ServiceNetworkSecurity, true},
		{ServiceWebProxy, ServiceSecureWebProxy, true},
		{ServiceURLList, ServiceNetworkSecurity, true},
		{ServiceTLSInspection, ServiceSecureWebProxy, false},
		{ServiceVPC, ServiceVPC, true},
		{ServiceSubnet, ServiceNetworkServices, false},
	} {
		if got := InProduct(tc.svc, tc.product); got != tc.want {
			t.Errorf("InProduct(%s, %s) = %v", tc.svc, tc.product, got)
		}
	}
	owner := Resource{ID: "projects/p", ScopeID: "projects/p"}
	if n := ProductGroupResource(owner, ServiceVPC, 1).Name; n != "VPC Networks" {
		t.Errorf("VPC node %q", n)
	}
}

func TestGroupByProductNetworkSecurity(t *testing.T) {
	t.Parallel()
	items := []Resource{
		{ID: "wp", Name: "egress-proxy", Service: ServiceWebProxy},
		{ID: "url", Name: "dev-tools", Service: ServiceURLList},
		{ID: "pol", Name: "egress-policy", Service: ServiceSWPPolicy},
		{ID: "tls", Name: "egress-tls", Service: ServiceTLSInspection},
		{ID: "ssl", Name: "modern", Service: ServiceSSLPolicy},
		{ID: "ca", Name: "web-armor", Service: ServiceCloudArmor},
		{ID: "iap", Name: "admin-backend", Service: ServiceIAPApp},
	}
	got := GroupByProduct(GroupByService(items))
	if len(got) != 1 || got[0].Product != ServiceNetworkSecurity {
		t.Fatalf("sections %+v", got)
	}
	services := func(groups []ServiceGroup) []ServiceType {
		var out []ServiceType
		for _, g := range groups {
			out = append(out, g.Service)
		}
		return out
	}
	sec := got[0]
	if want := []ServiceType{ServiceCloudArmor, ServiceSSLPolicy, ServiceTLSInspection, ServiceIAPApp}; !slices.Equal(services(sec.Groups), want) {
		t.Fatalf("network security groups %v, want %v", services(sec.Groups), want)
	}
	if len(sec.Subs) != 1 || sec.Subs[0].Product != ServiceSecureWebProxy {
		t.Fatalf("subs %+v", sec.Subs)
	}
	if want := []ServiceType{ServiceWebProxy, ServiceSWPPolicy, ServiceURLList}; !slices.Equal(services(sec.Subs[0].Groups), want) {
		t.Fatalf("secure web proxy groups %v, want %v", services(sec.Subs[0].Groups), want)
	}
}

func TestCollectionLabel(t *testing.T) {
	t.Parallel()
	if ServiceIAMBinding.CollectionLabel() != "Role Bindings" {
		t.Fatal(ServiceIAMBinding.CollectionLabel())
	}
	if spec, _ := Spec(ServiceIAMBinding); spec.Group != ServiceIAM {
		t.Fatalf("bindings group under %q, want IAM", spec.Group)
	}
}

func TestGroupByProduct(t *testing.T) {
	t.Parallel()
	items := []Resource{
		{ID: "b", Name: "bucket", Service: ServiceCloudStorage},
		{ID: "n", Name: "default", Service: ServiceVPC},
		{ID: "t", Name: "events", Service: ServicePubSub},
		{ID: "vm", Name: "web-1", Service: ServiceComputeEngine},
		{ID: "ip", Name: "web-ip", Service: ServiceAddress},
	}
	got := GroupByProduct(GroupByService(items))
	type section struct {
		product  ServiceType
		services []ServiceType
	}
	var sections []section
	for _, s := range got {
		sec := section{product: s.Product}
		for _, g := range s.Groups {
			sec.services = append(sec.services, g.Service)
		}
		sections = append(sections, sec)
	}
	want := []section{
		{"", []ServiceType{ServiceCloudStorage}},
		{"", []ServiceType{ServicePubSub}},
		{ServiceComputeEngine, []ServiceType{ServiceComputeEngine}},
		{ServiceVPC, []ServiceType{ServiceVPC, ServiceAddress}},
	}
	if len(sections) != len(want) {
		t.Fatalf("sections %+v", sections)
	}
	for i := range want {
		if sections[i].product != want[i].product || !slices.Equal(sections[i].services, want[i].services) {
			t.Errorf("section %d = %+v, want %+v", i, sections[i], want[i])
		}
	}
	if n := got[3].Items(); n != 2 { // VPC Networks and IP Addresses
		t.Errorf("Items()=%d, want 2", n)
	}
}

func TestProductGroupResource(t *testing.T) {
	t.Parallel()
	owner := HierarchyNode{ID: "projects/p", Name: "p", Kind: KindProject}.Resource()
	g := ProductGroupResource(owner, ServiceComputeEngine, 3)
	if !g.IsProductGroup() || g.IsCollection() || g.Name != "Compute Engine" || g.ParentID != owner.ID || g.ScopeID != "projects/p" {
		t.Fatalf("%+v", g)
	}
	c := CollectionResource(g, ServiceVPC, 1)
	if c.ParentID != g.ID || c.ScopeID != "projects/p" || c.Name != "VPC Networks" {
		t.Fatalf("%+v", c)
	}
	if got := GroupByService([]Resource{g, c}); len(got) != 0 {
		t.Fatalf("synthetic nodes grouped: %+v", got)
	}
}

func TestSplitFirst(t *testing.T) {
	t.Parallel()
	groups := GroupByService([]Resource{
		{ID: "b", Name: "b", Service: ServiceCloudStorage},
		{ID: "n", Name: "n", Service: ServiceVPC},
		{ID: "r", Name: "r", Service: ServiceIAMBinding},
		{ID: "s", Name: "s", Service: ServiceServiceAccount},
	})
	first, rest := SplitFirst(GroupByProduct(groups))
	if len(first) != 1 || first[0].Product != ServiceIAM || len(first[0].Groups) != 2 {
		t.Fatalf("first %+v", first)
	}
	var got []ServiceType
	for _, s := range rest {
		if s.Product != "" {
			got = append(got, s.Product)
		} else {
			got = append(got, s.Groups[0].Service)
		}
	}
	if want := []ServiceType{ServiceCloudStorage, ServiceVPC}; !slices.Equal(got, want) {
		t.Errorf("rest %v, want %v", got, want)
	}
}

func TestCompareNatural(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.9", "v1.10", -1},
		{"v1.10.0", "v1.9.3", 1},
		{"build-2", "build-10", -1},
		{"1.2.3", "1.2.3", 0},
		{"V2", "v10", -1},
		{"app", "app-1", -1},
		{"1.02", "1.2", -1}, // equal numbers: plain comparison breaks the tie
		{"rc", "1.0", 1},
		{"20260105", "20251231", 1},
	}
	for _, tt := range tests {
		if got := CompareNatural(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareNatural(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := CompareNatural(tt.b, tt.a); got != -tt.want {
			t.Errorf("CompareNatural(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
}

func TestSortNatural(t *testing.T) {
	t.Parallel()
	items := []Resource{{Name: "web-10"}, {Name: "Web-9"}, {Name: "api"}, {Name: "web-1"}}
	SortNatural(items)
	var got []string
	for _, r := range items {
		got = append(got, r.Name)
	}
	if want := []string{"api", "web-1", "Web-9", "web-10"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
