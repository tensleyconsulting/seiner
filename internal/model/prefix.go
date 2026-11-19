package model

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	// MetaKind distinguishes synthetic explorer nodes in Metadata.
	MetaKind = "kind"
	// KindPrefix is a GCS-style folder node (object name prefix).
	KindPrefix = "prefix"
	// KindCollection groups resources of one service under a CRM node.
	KindCollection = "collection"
	// KindProduct holds several collections of one product (Compute
	// Engine holds GCE Instances; VPC Network holds VPC Networks and IP
	// Addresses).
	KindProduct = "product"
	// KindEnabled is the project "Enabled Services" explorer node.
	KindEnabled = "enabled-services"
	// KindURLListItem is one URL/host pattern inside a Secure Web Proxy URL list.
	KindURLListItem = "url-list-item"
	// MetaPrefix is the full object prefix, including the trailing slash.
	MetaPrefix = "prefix"
	// MetaPath is the original object name before basename display.
	MetaPath = "path"
)

// IsPrefix reports whether r is a synthetic folder node.
func (r Resource) IsPrefix() bool {
	return r.Metadata[MetaKind] == KindPrefix
}

// IsCollection reports whether r is a service grouping node (Cloud Storage, …).
func (r Resource) IsCollection() bool {
	return r.Metadata[MetaKind] == KindCollection
}

// IsProductGroup reports whether r is a product node that holds
// collections (Compute Engine, …).
func (r Resource) IsProductGroup() bool {
	return r.Metadata[MetaKind] == KindProduct
}

// IsEnabledGroup reports whether r is the Enabled Services node.
func (r Resource) IsEnabledGroup() bool {
	return r.Metadata[MetaKind] == KindEnabled
}

// IsURLListItem reports whether r is a synthetic row for one entry of a
// Secure Web Proxy URL list.
func (r Resource) IsURLListItem() bool {
	return r.Metadata[MetaKind] == KindURLListItem
}

// IsSynthetic reports whether r is an explorer node with no ID of its own in
// Google Cloud: a grouping node (collection, product, folder prefix,
// principal, severity, bucket), an IAM permission, or a URL list entry.
func (r Resource) IsSynthetic() bool {
	return r.IsCollection() || r.IsProductGroup() || r.IsEnabledGroup() || r.IsPrefix() ||
		r.IsPrincipal() || r.IsInheritedGroup() || r.IsSeverityGroup() ||
		r.IsPermissionGroup() || r.IsPermission() || r.IsBucket() || r.IsURLListItem()
}

// URLListItemResource is a synthetic row for the index-th pattern of list.
func URLListItemResource(list Resource, value string, index int) Resource {
	md := map[string]string{
		MetaKind:  KindURLListItem,
		"urlList": list.Name,
	}
	return Resource{
		ID:       list.ID + "/values/" + strconv.Itoa(index+1),
		Name:     value,
		Service:  ServiceURLList,
		ScopeID:  list.ScopeID,
		Location: list.Location,
		ParentID: list.ID,
		Metadata: md,
		Fields:   []Field{{Key: "urlList", Value: list.Name}},
	}
}

// PrefixNode is one folder or leaf in a slash-separated object tree.
type PrefixNode struct {
	Resource Resource
	Children []PrefixNode
}

type prefixAcc struct {
	node     PrefixNode
	children map[string]*prefixAcc
	order    []string
	leaves   []PrefixNode
}

// NestPrefixes groups items whose names contain '/' into folder nodes.
// Folder nodes are synthetic; leaves keep their identity and show the basename.
func NestPrefixes(owner Resource, items []Resource) []PrefixNode {
	root := &prefixAcc{children: map[string]*prefixAcc{}}
	for _, item := range items {
		segs := pathSegments(item.Name)
		if len(segs) == 0 {
			continue
		}
		cur := root
		prefix := ""
		for i := 0; i < len(segs)-1; i++ {
			seg := segs[i]
			prefix += seg + "/"
			next, ok := cur.children[seg]
			if !ok {
				parentID := owner.ID
				if cur != root {
					parentID = cur.node.Resource.ID
				}
				next = &prefixAcc{
					node:     PrefixNode{Resource: PrefixResource(owner, prefix, seg, parentID)},
					children: map[string]*prefixAcc{},
				}
				cur.children[seg] = next
				cur.order = append(cur.order, seg)
			}
			cur = next
		}
		cur.leaves = append(cur.leaves, PrefixNode{Resource: leafDisplay(item, segs[len(segs)-1])})
	}
	return root.flatten()
}

// PrefixResource is a synthetic folder under owner.
func PrefixResource(owner Resource, prefix, name, parentID string) Resource {
	if parentID == "" {
		parentID = owner.ID
	}
	return Resource{
		ID:       owner.ID + "/prefixes/" + url.PathEscape(prefix),
		Name:     name,
		Service:  owner.Service,
		ScopeID:  owner.ScopeID,
		Location: owner.Location,
		Metadata: map[string]string{
			MetaKind:   KindPrefix,
			MetaPrefix: prefix,
		},
		ParentID: parentID,
		FacetID:  FacetObjects,
	}
}

func leafDisplay(item Resource, base string) Resource {
	out := item
	out.Name = base
	md := make(map[string]string, len(item.Metadata)+1)
	for k, v := range item.Metadata {
		md[k] = v
	}
	if item.Name != base {
		md[MetaPath] = item.Name
		if len(item.Fields) > 0 {
			out.Fields = append(slices.Clone(item.Fields), Field{Key: MetaPath, Value: item.Name})
		}
	}
	out.Metadata = md
	return out
}

func (a *prefixAcc) flatten() []PrefixNode {
	out := make([]PrefixNode, 0, len(a.order)+len(a.leaves))
	for _, seg := range a.order {
		child := a.children[seg]
		n := child.node
		n.Children = child.flatten()
		out = append(out, n)
	}
	return append(out, a.leaves...)
}

func pathSegments(name string) []string {
	var segs []string
	for _, p := range strings.Split(name, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	return segs
}
