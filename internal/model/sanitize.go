package model

import (
	"strings"
	"unicode/utf8"
)

// cleanLine strips terminal escape sequences, control characters, and Unicode
// bidirectional overrides from s, collapsing CR, LF, and TAB into single
// spaces so s is safe to render on a single terminal line.
func cleanLine(s string) string {
	if !needsClean(s, false) {
		return s
	}
	return filterControls(stripANSI(s), false)
}

// cleanText strips terminal escape sequences, control characters, and Unicode
// bidirectional overrides from s while preserving LF line breaks (converting
// CRLF and bare CR to LF, and TAB to a space).
func cleanText(s string) string {
	if !needsClean(s, true) {
		return s
	}
	return filterControls(stripANSI(s), true)
}

func needsClean(s string, multiline bool) bool {
	hasNonASCII := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b >= 0x80 {
			hasNonASCII = true
			break
		}
		if (b < 0x20 && (!multiline || b != '\n')) || b == 0x7f {
			return true
		}
	}
	if !hasNonASCII {
		return false
	}
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && width == 1 {
			return true
		}
		if (r < 0x20 && (!multiline || r != '\n')) || r == 0x7f || isC1(r) || isBiDiControl(r) {
			return true
		}
		i += width
	}
	return false
}

func isC1(r rune) bool {
	return r >= 0x80 && r <= 0x9f
}

// isBiDiControl reports Unicode bidirectional formatting/override controls
// (Trojan Source / visual spoofing characters).
func isBiDiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f ||
		(r >= 0x202a && r <= 0x202e) ||
		(r >= 0x2066 && r <= 0x2069)
}

// stripANSI removes 7-bit ESC sequences and 8-bit C1 CSI/OSC/DCS/APC/PM/SOS
// sequences so payloads like "\x1b[31mred" or "\x1b]52;c;...\x07" drop the
// entire escape sequence rather than leaving parameter text behind.
func stripANSI(s string) string {
	if !hasEscapeIntroducer(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b:
			i = skipEsc(s, i+1)
		case r == 0x9b: // C1 CSI
			i = skipCSI(s, i+width)
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f: // C1 DCS/SOS/OSC/PM/APC
			i = skipControlString(s, i+width)
		default:
			b.WriteString(s[i : i+width])
			i += width
		}
	}
	return b.String()
}

func hasEscapeIntroducer(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b || s[i] == 0xc2 { // U+0080..U+009F encode as 0xC2 0x80..0x9F in UTF-8
			return true
		}
	}
	return false
}

func skipEsc(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[': // CSI
		return skipCSI(s, i+1)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC
		return skipControlString(s, i+1)
	default:
		// nF / two-byte escape: optional intermediate bytes 0x20..0x2f, then final byte 0x30..0x7e.
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
			i++
		}
		return i
	}
}

func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
		i++
	}
	return i
}

func skipControlString(s string, i int) int {
	for i < len(s) {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r == 0x07 || r == 0x9c { // BEL or 8-bit ST
			return i + width
		}
		if r == 0x1b {
			if i+1 < len(s) && s[i+1] == '\\' { // 7-bit ST (ESC \)
				return i + 2
			}
			return i
		}
		i += width
	}
	return i
}

func filterControls(s string, multiline bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			if multiline {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		r, width := utf8.DecodeRuneInString(s[i:])
		i += width
		if r == utf8.RuneError && width == 1 {
			b.WriteRune(utf8.RuneError)
			continue
		}
		switch {
		case r == '\n':
			if multiline {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || isC1(r) || isBiDiControl(r):
			// Strip remaining control and bidirectional override runes.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeResource(r Resource) Resource {
	hadName := r.Name != ""
	r.ID = cleanLine(r.ID)
	r.Name = cleanLine(r.Name)
	if hadName && r.Name == "" {
		r.Name = "(unnamed)"
	}
	r.ScopeID = cleanLine(r.ScopeID)
	r.Location = cleanLine(r.Location)
	r.ParentID = cleanLine(r.ParentID)
	r.FacetID = cleanLine(r.FacetID)
	r.State = cleanLine(r.State)
	r.Badge = cleanLine(r.Badge)
	r.Description = cleanText(r.Description)
	r.Metadata = sanitizeMap(r.Metadata, true)
	r.Labels = sanitizeMap(r.Labels, false)
	r.Fields = sanitizeFields(r.Fields)
	r.Facets = sanitizeFacets(r.Facets)
	r.Lists = sanitizeLists(r.Lists)
	r.Copies = sanitizeCopies(r.Copies)
	return r
}

func sanitizeMap(m map[string]string, multilineValues bool) map[string]string {
	if len(m) == 0 {
		return m
	}
	dirty := false
	for k, v := range m {
		valDirty := needsClean(v, multilineValues && !internalKey(k))
		if needsClean(k, false) || valDirty {
			dirty = true
			break
		}
	}
	if !dirty {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		ck := cleanLine(k)
		var cv string
		if multilineValues && !internalKey(ck) {
			cv = cleanText(v)
		} else {
			cv = cleanLine(v)
		}
		out[ck] = cv
	}
	return out
}

func sanitizeFields(fields []Field) []Field {
	var out []Field
	for i, f := range fields {
		ck, cv := cleanLine(f.Key), cleanText(f.Value)
		if out == nil && (ck != f.Key || cv != f.Value) {
			out = make([]Field, len(fields))
			copy(out, fields[:i])
		}
		if out != nil {
			out[i] = Field{Key: ck, Value: cv}
		}
	}
	if out != nil {
		return out
	}
	return fields
}

func sanitizeFacets(facets []Facet) []Facet {
	var out []Facet
	for i, f := range facets {
		cid, clabel := cleanLine(f.ID), cleanLine(f.Label)
		if out == nil && (cid != f.ID || clabel != f.Label) {
			out = make([]Facet, len(facets))
			copy(out, facets[:i])
		}
		if out != nil {
			nf := f
			nf.ID = cid
			nf.Label = clabel
			out[i] = nf
		}
	}
	if out != nil {
		return out
	}
	return facets
}

func sanitizeLists(lists []List) []List {
	var out []List
	for i, l := range lists {
		cid, clabel := cleanLine(l.ID), cleanLine(l.Label)
		citems := sanitizeStrings(l.Items)
		changed := cid != l.ID || clabel != l.Label || (len(citems) > 0 && &citems[0] != &l.Items[0])
		if out == nil && changed {
			out = make([]List, len(lists))
			copy(out, lists[:i])
		}
		if out != nil {
			out[i] = List{ID: cid, Label: clabel, Items: citems}
		}
	}
	if out != nil {
		return out
	}
	return lists
}

func sanitizeStrings(items []string) []string {
	var out []string
	for i, s := range items {
		cs := cleanText(s)
		if out == nil && cs != s {
			out = make([]string, len(items))
			copy(out, items[:i])
		}
		if out != nil {
			out[i] = cs
		}
	}
	if out != nil {
		return out
	}
	return items
}

func sanitizeCopies(copies []Copy) []Copy {
	var out []Copy
	for i, c := range copies {
		ctitle, clabel, ctext := cleanLine(c.Title), cleanLine(c.Label), cleanText(c.Text)
		if out == nil && (ctitle != c.Title || clabel != c.Label || ctext != c.Text) {
			out = make([]Copy, len(copies))
			copy(out, copies[:i])
		}
		if out != nil {
			out[i] = Copy{Title: ctitle, Label: clabel, Text: ctext}
		}
	}
	if out != nil {
		return out
	}
	return copies
}
