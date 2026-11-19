// Package pagetest is an httptest server for wire-level tests of the
// adapters' REST clients. It serves paged JSON lists, single JSON bodies,
// and Google-style errors by path suffix, and records what each route was
// asked for. Only tests import it.
package pagetest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Server routes a request to the registered route with the longest path
// suffix that matches. An unmatched path is a 404.
type Server struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]route
	asked  map[string][]url.Values
}

type route struct {
	field  string   // list field name; "" for a single body or raw pages
	pages  []string // JSON items of each page, comma-joined; or raw page objects
	body   string
	status int
}

// New starts a server that the test closes when it ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{routes: map[string]route{}, asked: map[string][]url.Values{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// Pages serves a list at suffix whose items, in the JSON field named field,
// come in pages. The first page needs no pageToken; page i is "p<i>", and
// every page but the last links to the next with nextPageToken.
func (s *Server) Pages(suffix, field string, pages ...[]string) {
	r := route{field: field, status: http.StatusOK}
	for _, p := range pages {
		r.pages = append(r.pages, strings.Join(p, ","))
	}
	s.set(suffix, r)
}

// RawPages serves a list at suffix whose pages are whole JSON objects, such
// as Compute's aggregated lists, whose items are a map. Each page but the
// last gets a nextPageToken; tokens are as for Pages.
func (s *Server) RawPages(suffix string, pages ...string) {
	s.set(suffix, route{pages: pages, status: http.StatusOK})
}

// JSON serves body at suffix.
func (s *Server) JSON(suffix, body string) {
	s.set(suffix, route{body: body, status: http.StatusOK})
}

// Error serves a Google API error with code and reason at suffix.
func (s *Server) Error(suffix string, code int, reason string) {
	body := fmt.Sprintf(`{"error":{"code":%d,"message":%q,"errors":[{"reason":%q,"message":%q}]}}`,
		code, http.StatusText(code), reason, http.StatusText(code))
	s.set(suffix, route{body: body, status: code})
}

// Asked is the query of each request a route received, in order.
func (s *Server) Asked(suffix string) []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked[suffix])
}

// Tokens is the pageToken of each request a route received ("" for the
// first page).
func (s *Server) Tokens(suffix string) []string {
	var out []string
	for _, q := range s.Asked(suffix) {
		out = append(out, q.Get("pageToken"))
	}
	return out
}

func (s *Server) set(suffix string, r route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[suffix] = r
}

func (s *Server) serve(w http.ResponseWriter, req *http.Request) {
	s.mu.Lock()
	suffix := ""
	for k := range s.routes {
		if strings.HasSuffix(req.URL.Path, k) && len(k) > len(suffix) {
			suffix = k
		}
	}
	r, ok := s.routes[suffix]
	if ok {
		s.asked[suffix] = append(s.asked[suffix], req.URL.Query())
	}
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	if r.pages == nil {
		fmt.Fprint(w, r.body)
		return
	}
	i := 0
	if tok := req.URL.Query().Get("pageToken"); tok != "" {
		if _, err := fmt.Sscanf(tok, "p%d", &i); err != nil || i >= len(r.pages) {
			http.Error(w, "bad page token "+tok, http.StatusBadRequest)
			return
		}
	}
	next := ""
	if i+1 < len(r.pages) {
		next = fmt.Sprintf(`"nextPageToken":"p%d"`, i+1)
	}
	if r.field == "" {
		page := strings.TrimSpace(r.pages[i])
		if next != "" {
			page = strings.TrimSuffix(page, "}") + "," + next + "}"
		}
		fmt.Fprint(w, page)
		return
	}
	if next != "" {
		next = "," + next
	}
	fmt.Fprintf(w, `{%q:[%s]%s}`, r.field, r.pages[i], next)
}
