package task

import "slices"

type Permission uint8

const (
	PermView Permission = 1 << iota
	PermChangeStatus
	PermEditContent
	PermAssign
	PermViewShares
	PermDelete
	PermManageShares
)

type Role string

const (
	RoleOwner       Role = "owner"
	RoleManager     Role = "manager"
	RoleEditor      Role = "editor"
	RoleContributor Role = "contributor"
	RoleViewer      Role = "viewer"
)

var grantableRoles = []Role{
	RoleManager, RoleEditor, RoleContributor, RoleViewer,
}

var rolePerms = map[Role]Permission{
	RoleViewer:      PermView | PermViewShares,
	RoleContributor: PermView | PermViewShares | PermChangeStatus,
	RoleEditor:      PermView | PermViewShares | PermChangeStatus | PermEditContent,
	RoleManager:     PermView | PermViewShares | PermChangeStatus | PermEditContent | PermAssign,
	RoleOwner:       PermView | PermViewShares | PermChangeStatus | PermEditContent | PermAssign | PermDelete | PermManageShares,
}

func (r Role) Can(p Permission) bool {
	return rolePerms[r]&p == p
}

func (r Role) IsGrantable() bool {
	return slices.Contains(grantableRoles, r)
}

func RequiredForPatch(p Patch) Permission {
	var need Permission
	if p.Title != nil || p.Description != nil || p.DueAt != nil {
		need |= PermEditContent
	}
	if p.Status != nil {
		need |= PermChangeStatus
	}

	return need
}
