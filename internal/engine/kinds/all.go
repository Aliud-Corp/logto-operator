// Package kinds lists every kind of the engine; each lives in its own package below.
package kinds

import (
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/applications"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/organizations"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/profilefields"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/signinexperience"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/users"
)

// All is every kind, in the reconciliation order of the plan (engine.New orders them by dependency anyway).
func All() []engine.Kind {
	return []engine.Kind{
		connectors.Kind{},
		profilefields.Kind{},
		signinexperience.Kind{},
		resources.Kind{},
		organizations.Scopes{},
		organizations.Roles{},
		roles.Kind{},
		applications.Kind{},
		users.Kind{},
	}
}
