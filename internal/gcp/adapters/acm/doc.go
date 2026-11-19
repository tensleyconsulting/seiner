// Package acm adapts Access Context Manager access bindings (GCP user
// access bindings) to model.Resource. A binding holds a Google group, or a
// service account, to access levels when it uses the Google Cloud console
// and APIs. Bindings belong to an organization. The API names a group by
// its opaque id, so the adapter looks up each group's email through Cloud
// Identity and falls back to the id when it cannot.
package acm
