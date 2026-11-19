package fake

import (
	"context"
	"slices"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// fixturePermissions are the list permissions the fixture providers declare,
// matching their live adapters. Only the services denied somewhere need one.
var fixturePermissions = map[model.ServiceType][]string{
	model.ServiceCloudSQL:      {"cloudsql.instances.list"},
	model.ServiceSecretManager: {"secretmanager.secrets.list"},
}

// denied are the permissions the fixture caller lacks, by scope:
// checkout-project shows "No access" details for Cloud SQL and Secrets.
var denied = map[string][]string{
	"projects/checkout-project": {"cloudsql.instances.list", "secretmanager.secrets.list"},
}

// Permissions is the fixture permission tester. It grants every permission
// except those denied on the scope.
func Permissions() model.PermissionTester { return permissionTester{} }

type permissionTester struct{}

func (permissionTester) TestPermissions(ctx context.Context, scopeID string, perms []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(slices.Clone(perms), func(p string) bool {
		return slices.Contains(denied[scopeID], p)
	}), nil
}
