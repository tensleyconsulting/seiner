// Package iap adapts Identity-Aware Proxy to model.Resource. The IAP API
// cannot list what it secures, so the adapter finds the load balancer
// backend services that have IAP turned on, reads each one's IAP settings,
// and lists who holds IAP roles on it.
package iap
