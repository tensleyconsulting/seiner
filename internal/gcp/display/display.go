// Package display formats API values the way adapters show them in a
// resource's details fields, so every adapter reads alike.
package display

import (
	"strings"
	"time"
)

// Stamp formats t to the minute in UTC ("2026-11-08 03:00 UTC"); "" when t
// is zero.
func Stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// RelLink turns a Google self link into its relative name, from
// "projects/" on, so links from different hosts or API versions compare
// equal. A link without "projects/" is returned as-is.
func RelLink(link string) string {
	if i := strings.Index(link, "projects/"); i >= 0 {
		return link[i:]
	}
	return link
}
