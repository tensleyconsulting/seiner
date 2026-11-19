package model

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// KindBucket is a synthetic node holding some of a Bucketed type's
	// resources: those in one folder (a Workspace org unit), or those
	// whose names start with the same letters.
	KindBucket = "bucket"
	// MetaBucket is a bucket node's key: its folder path ("/Sales"), with
	// "|" and a name prefix for a letter bucket ("/Sales|j").
	MetaBucket = "bucket"
	// MetaOrgUnit is a Workspace user's org unit path ("/Sales/EMEA"),
	// the FolderKey of Workspace users.
	MetaOrgUnit = "orgUnit"
	// BucketMax is the most resources a bucketed list shows flat at one
	// level. Past it they go in letter buckets.
	BucketMax = 50
	// bucketLetters is how many leading letters letter buckets go down to
	// ("j", then "ja").
	bucketLetters = 2
)

// IsBucket reports whether r is a synthetic node holding some of a
// Bucketed type's resources.
func (r Resource) IsBucket() bool {
	return r.Metadata[MetaKind] == KindBucket
}

// Bucketed reports whether svc's resources are laid out with Buckets.
func Bucketed(svc ServiceType) bool {
	spec, ok := Spec(svc)
	return ok && spec.Bucketed
}

// BucketNode is one row of a bucketed list: a bucket with Children, or a
// single resource.
type BucketNode struct {
	Resource Resource
	Children []BucketNode
}

// Buckets lays out items, resources of one Bucketed type, under owner (a
// collection, or the resource whose facetID lists them), sorted by name.
// When the type has a FolderKey, items are filed in a folder per path
// segment, and the items at the root go in a "/" folder when there are
// other folders beside it. A folder lists its subfolders, then its own
// items. Up to BucketMax items at one level list flat; more go in one
// bucket per first letter, and a letter with more than that in one per
// first two letters. A level with a single bucket is skipped, since it
// would only add a row.
func Buckets(owner Resource, facetID string, items []Resource) []BucketNode {
	if len(items) == 0 {
		return nil
	}
	items = slices.Clone(items)
	SortByName(items)
	b := bucketer{owner: owner, facet: facetID, svc: items[0].Service}
	key := ""
	if spec, ok := Spec(items[0].Service); ok {
		key = spec.FolderKey
	}
	if key == "" {
		return b.letters(owner.ID, "", items, 1)
	}
	root := &folder{}
	for _, r := range items {
		root.add(pathSegments(r.Metadata[key]), r)
	}
	if len(root.order) == 0 {
		return b.letters(owner.ID, "/", root.items, 1)
	}
	out := b.folders(owner.ID, key, root)
	if len(root.items) > 0 {
		n := b.node(owner.ID, "/", "/", len(root.items))
		n.Resource.Fields = slices.Insert(n.Resource.Fields, 0, Field{Key: key, Value: "/"})
		n.Children = b.letters(n.Resource.ID, "/", root.items, 1)
		out = append(out, n)
	}
	return out
}

// folder is one path segment's items and subfolders.
type folder struct {
	path  string
	items []Resource
	kids  map[string]*folder
	order []string
	total int
}

func (f *folder) add(segs []string, r Resource) {
	f.total++
	if len(segs) == 0 {
		f.items = append(f.items, r)
		return
	}
	kid, ok := f.kids[segs[0]]
	if !ok {
		if f.kids == nil {
			f.kids = map[string]*folder{}
		}
		kid = &folder{path: f.path + "/" + segs[0]}
		f.kids[segs[0]] = kid
		f.order = append(f.order, segs[0])
	}
	kid.add(segs[1:], r)
}

type bucketer struct {
	owner Resource
	facet string
	svc   ServiceType
}

// folders lays out f's subfolders, each holding its own subfolders and
// then its items.
func (b bucketer) folders(parentID, key string, f *folder) []BucketNode {
	names := slices.Clone(f.order)
	slices.SortFunc(names, func(x, y string) int {
		return strings.Compare(strings.ToLower(x), strings.ToLower(y))
	})
	out := make([]BucketNode, 0, len(names))
	for _, name := range names {
		kid := f.kids[name]
		n := b.node(parentID, kid.path, name, kid.total)
		n.Resource.Fields = slices.Insert(n.Resource.Fields, 0, Field{Key: key, Value: kid.path})
		n.Children = append(b.folders(n.Resource.ID, key, kid), b.letters(n.Resource.ID, kid.path, kid.items, 1)...)
		out = append(out, n)
	}
	return out
}

// letters lays out items, sorted by name and all in the folder at path,
// grouping them by their first n letters when there are too many.
func (b bucketer) letters(parentID, path string, items []Resource, n int) []BucketNode {
	if len(items) <= BucketMax || n > bucketLetters {
		out := make([]BucketNode, len(items))
		for i, r := range items {
			out[i] = BucketNode{Resource: r}
		}
		return out
	}
	var prefixes []string
	by := map[string][]Resource{}
	for _, r := range items {
		p := namePrefix(r.Name, n)
		if _, ok := by[p]; !ok {
			prefixes = append(prefixes, p)
		}
		by[p] = append(by[p], r)
	}
	if len(prefixes) == 1 {
		return b.letters(parentID, path, items, n+1)
	}
	out := make([]BucketNode, 0, len(prefixes))
	for _, p := range prefixes {
		kids := by[p]
		node := b.node(parentID, path+"|"+p, prefixLabel(p), len(kids))
		node.Resource.Fields = slices.Insert(node.Resource.Fields, 0, Field{Key: "startsWith", Value: prefixLabel(p)})
		node.Children = b.letters(node.Resource.ID, path, kids, n+1)
		out = append(out, node)
	}
	return out
}

// node is the bucket keyed key, named name, holding n resources under the
// row parentID.
func (b bucketer) node(parentID, key, name string, n int) BucketNode {
	count := strconv.Itoa(n)
	return BucketNode{Resource: Resource{
		ID:       b.owner.ID + "/buckets/" + url.PathEscape(key),
		Name:     name,
		Service:  b.svc,
		ScopeID:  b.owner.ScopeID,
		Location: b.owner.Location,
		ParentID: parentID,
		FacetID:  b.facet,
		Badge:    count,
		Metadata: map[string]string{MetaKind: KindBucket, MetaBucket: key},
		Fields:   []Field{{Key: strings.ToLower(b.svc.CollectionLabel()), Value: count}},
	}}
}

// namePrefix is the first n letters of name, lower-cased.
func namePrefix(name string, n int) string {
	name = strings.ToLower(name)
	i := 0
	for range n {
		if i >= len(name) {
			break
		}
		_, size := utf8.DecodeRuneInString(name[i:])
		i += size
	}
	return name[:i]
}

// prefixLabel names a letter bucket: "J", "Ja".
func prefixLabel(p string) string {
	r, size := utf8.DecodeRuneInString(p)
	return strings.ToUpper(string(r)) + p[size:]
}
