package serviceusage

import (
	"context"
	"strings"

	"google.golang.org/api/option"
	gapi "google.golang.org/api/serviceusage/v1"

	"github.com/tensleyconsulting/seiner/internal/gcp/lazy"
)

type liveAPI struct {
	svc *gapi.Service
}

// NewLazy returns a Provider whose Service Usage client is constructed on
// first use rather than at startup. A construction failure is returned from
// that first call instead of blocking Open or silently dropping the service.
func NewLazy(opts ...option.ClientOption) Provider {
	l := lazy.New(func(ctx context.Context) (*gapi.Service, error) {
		return gapi.NewService(ctx, opts...)
	})
	return New(lazyAPI{l: l})
}

type lazyAPI struct {
	l *lazy.Value[*gapi.Service]
}

func (a lazyAPI) ListEnabled(ctx context.Context, project string) ([]APIService, error) {
	svc, err := a.l.Get(ctx)
	if err != nil {
		return nil, err
	}
	return liveAPI{svc: svc}.ListEnabled(ctx, project)
}

var _ API = lazyAPI{}

func (a liveAPI) ListEnabled(ctx context.Context, project string) ([]APIService, error) {
	parent := "projects/" + project
	var out []APIService
	err := a.svc.Services.List(parent).Filter("state:ENABLED").Pages(ctx, func(resp *gapi.ListServicesResponse) error {
		for _, s := range resp.Services {
			if s == nil {
				continue
			}
			api := APIService{Name: apiName(s.Name)}
			if c := s.Config; c != nil {
				api.Title = c.Title
				if api.Name == "" {
					api.Name = apiName(c.Name)
				}
				if d := c.Documentation; d != nil {
					api.Summary = d.Summary
					api.DocsURL = d.DocumentationRootUrl
				}
			}
			if api.Name == "" {
				continue
			}
			out = append(out, api)
		}
		return nil
	})
	return out, err
}

func apiName(resourceName string) string {
	resourceName = strings.TrimSpace(resourceName)
	if i := strings.LastIndex(resourceName, "/services/"); i >= 0 {
		return resourceName[i+len("/services/"):]
	}
	if strings.Contains(resourceName, ".googleapis.com") {
		return resourceName
	}
	return ""
}
