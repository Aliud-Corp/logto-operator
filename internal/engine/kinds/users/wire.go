package users

// The kind's own wire types. A user is never sent a password.

type wireUser struct {
	ID           string  `json:"id"`
	PrimaryEmail *string `json:"primaryEmail"`
	Name         *string `json:"name"`
}

type createUser struct {
	PrimaryEmail string `json:"primaryEmail"`
	Name         string `json:"name,omitempty"`
}

type updateUser struct {
	Name string `json:"name"`
}

type wireRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type assignRoles struct {
	RoleIDs []string `json:"roleIds"`
}
