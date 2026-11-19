package model

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// ServiceGroup is one non-empty service bucket under a CRM node.
type ServiceGroup struct {
	Service ServiceType
	Items   []Resource
}

// SortByName sorts items by Name, case-insensitively and stably.
func SortByName(items []Resource) {
	slices.SortStableFunc(items, func(a, b Resource) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

// CompareNatural orders a and b case-insensitively, reading runs of digits
// as numbers: v1.9 < v1.10 and build-2 < build-10. Equal numbers with
// different leading zeros fall back to plain comparison.
func CompareNatural(a, b string) int {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		if isDigit(la[i]) && isDigit(lb[j]) {
			si, sj := i, j
			for i < len(la) && isDigit(la[i]) {
				i++
			}
			for j < len(lb) && isDigit(lb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(la[si:i], "0"), strings.TrimLeft(lb[sj:j], "0")
			if c := cmp.Compare(len(na), len(nb)); c != 0 {
				return c
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if c := cmp.Compare(la[i], lb[j]); c != 0 {
			return c
		}
		i++
		j++
	}
	if c := cmp.Compare(len(la)-i, len(lb)-j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// SortNatural sorts items by Name with CompareNatural, stably.
func SortNatural(items []Resource) {
	slices.SortStableFunc(items, func(a, b Resource) int { return CompareNatural(a.Name, b.Name) })
}

// GroupByService clusters resources by ServiceType. Empty groups are omitted.
// Organization, folder, and project nodes are skipped (they stay ungrouped).
// Items within a group are sorted by Name, case-insensitively and stably, so
// the explorer order does not depend on API response order.
func GroupByService(items []Resource) []ServiceGroup {
	buckets := make(map[ServiceType][]Resource)
	for _, r := range items {
		if r.IsScope() || r.IsCollection() || r.IsProductGroup() || r.IsEnabledGroup() {
			continue
		}
		buckets[r.Service] = append(buckets[r.Service], r)
	}
	svcs := make([]ServiceType, 0, len(buckets))
	for svc, kids := range buckets {
		if len(kids) > 0 && isGroupedService(svc) {
			SortByName(kids)
			svcs = append(svcs, svc)
		}
	}
	SortServices(svcs)
	out := make([]ServiceGroup, len(svcs))
	for i, svc := range svcs {
		out[i] = ServiceGroup{Service: svc, Items: buckets[svc]}
	}
	return out
}

// isGroupedService defaults to true so unregistered services still get a row.
func isGroupedService(svc ServiceType) bool {
	if spec, ok := Spec(svc); ok {
		return spec.Grouped
	}
	return true
}

// ProductSection is one explorer row under a CRM node. With Product set it
// is a product node holding Groups and then Subs, the products nested in
// it; otherwise Groups is a single ungrouped service group.
type ProductSection struct {
	Product ServiceType
	Groups  []ServiceGroup
	Subs    []ProductSection
}

// Items counts the resources across the section's groups, nested
// products included.
func (s ProductSection) Items() int {
	n := 0
	for _, g := range s.Groups {
		n += len(g.Items)
	}
	for _, sub := range s.Subs {
		n += sub.Items()
	}
	return n
}

// GroupByProduct folds service groups into sections by ServiceSpec.Group.
// A product's section takes the place of its first group, and groups keep
// their order inside it. A group whose product is itself grouped (Private
// Service Connect under Network Services) goes in a nested section, listed
// after the outer product's own groups.
func GroupByProduct(groups []ServiceGroup) []ProductSection {
	var out []ProductSection
	for _, g := range groups {
		product := productOf(g.Service)
		if product == "" {
			out = append(out, ProductSection{Groups: []ServiceGroup{g}})
			continue
		}
		outer, inner := product, ServiceType("")
		if p := outerProduct(product); p != "" {
			outer, inner = p, product
		}
		sec := section(&out, outer)
		if inner != "" {
			sec = section(&sec.Subs, inner)
		}
		sec.Groups = append(sec.Groups, g)
	}
	return out
}

// section returns product's section in secs, appending it when missing.
func section(secs *[]ProductSection, product ServiceType) *ProductSection {
	for i := range *secs {
		if (*secs)[i].Product == product {
			return &(*secs)[i]
		}
	}
	*secs = append(*secs, ProductSection{Product: product})
	return &(*secs)[len(*secs)-1]
}

// InProduct reports whether svc's collection sits under product's node,
// directly or in a product nested in it.
func InProduct(svc, product ServiceType) bool {
	p := productOf(svc)
	return p != "" && (p == product || outerProduct(p) == product)
}

// SplitFirst separates the sections whose product (or ungrouped service)
// is marked First from the rest, keeping order within each.
func SplitFirst(sections []ProductSection) (first, rest []ProductSection) {
	for _, s := range sections {
		svc := s.Product
		if svc == "" && len(s.Groups) > 0 {
			svc = s.Groups[0].Service
		}
		if spec, ok := Spec(svc); ok && spec.First {
			first = append(first, s)
		} else {
			rest = append(rest, s)
		}
	}
	return first, rest
}

// outerProduct is the product a GroupOnly product's node nests under, or
// "". A product that lists its own resources may name itself as its Group
// (VPC Networks), which is not nesting.
func outerProduct(product ServiceType) ServiceType {
	if spec, ok := Spec(product); ok && spec.GroupOnly && spec.Group != product {
		return spec.Group
	}
	return ""
}

func productOf(svc ServiceType) ServiceType {
	if spec, ok := Spec(svc); ok {
		return spec.Group
	}
	return ""
}

// ProductGroupResource is the explorer node for product under owner,
// holding n resources across its collections.
func ProductGroupResource(owner Resource, product ServiceType, n int) Resource {
	return Normalize(Resource{
		ID:       ProductGroupID(owner.ID, product),
		Name:     product.NodeLabel(),
		Service:  product,
		ScopeID:  owner.ScopeID,
		Location: "global",
		ParentID: owner.ID,
		Metadata: map[string]string{MetaKind: KindProduct},
		Fields:   []Field{{Key: "Resources", Value: strconv.Itoa(n)}},
	})
}

// ProductGroupID is the ID of product's node under the node ownerID.
func ProductGroupID(ownerID string, product ServiceType) string {
	return ownerID + "/products/" + string(product)
}

// CollectionID is the ID of svc's collection under the node parentID.
func CollectionID(parentID string, svc ServiceType) string {
	return parentID + "/collections/" + string(svc)
}

// CollectionResource is the explorer parent for one service group of n
// resources. parent is the CRM node, or the product node that holds it.
func CollectionResource(parent Resource, svc ServiceType, n int) Resource {
	return Normalize(Resource{
		ID:       CollectionID(parent.ID, svc),
		Name:     svc.CollectionLabel(),
		Service:  svc,
		ScopeID:  parent.ScopeID,
		Location: "global",
		ParentID: parent.ID,
		Metadata: map[string]string{MetaKind: KindCollection},
		Fields:   []Field{{Key: "Resources", Value: strconv.Itoa(n)}},
	})
}

// EnabledServicesResource is the explorer node that lists enabled APIs.
func EnabledServicesResource(owner Resource) Resource {
	return Normalize(Resource{
		ID:       owner.ID + "/collections/" + FacetEnabledServices,
		Name:     "Enabled Services",
		Service:  ServiceUsage,
		ScopeID:  owner.ScopeID,
		Location: "global",
		ParentID: owner.ID,
		Metadata: map[string]string{MetaKind: KindEnabled},
		Facets:   []Facet{{ID: FacetEnabledServices, Label: "Enabled Services"}},
	})
}
