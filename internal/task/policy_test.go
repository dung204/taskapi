package task_test

import (
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

func TestRoleCan(t *testing.T) {
	t.Parallel()
	allPerms := []task.Permission{
		task.PermView,
		task.PermChangeStatus,
		task.PermEditContent,
		task.PermAssign,
		task.PermViewShares,
		task.PermDelete,
		task.PermManageShares,
	}

	tests := []struct {
		name string
		role task.Role
		want map[task.Permission]bool
	}{
		{
			name: "viewer",
			role: task.RoleViewer,
			want: map[task.Permission]bool{
				task.PermView:       true,
				task.PermViewShares: true,
			},
		},
		{
			name: "contributor",
			role: task.RoleContributor,
			want: map[task.Permission]bool{
				task.PermView:         true,
				task.PermViewShares:   true,
				task.PermChangeStatus: true,
			},
		},
		{
			name: "editor",
			role: task.RoleEditor,
			want: map[task.Permission]bool{
				task.PermView:         true,
				task.PermViewShares:   true,
				task.PermChangeStatus: true,
				task.PermEditContent:  true,
			},
		},
		{
			name: "manager",
			role: task.RoleManager,
			want: map[task.Permission]bool{
				task.PermView:         true,
				task.PermViewShares:   true,
				task.PermChangeStatus: true,
				task.PermEditContent:  true,
				task.PermAssign:       true,
			},
		},
		{
			name: "owner",
			role: task.RoleOwner,
			want: map[task.Permission]bool{
				task.PermView:         true,
				task.PermViewShares:   true,
				task.PermChangeStatus: true,
				task.PermEditContent:  true,
				task.PermAssign:       true,
				task.PermDelete:       true,
				task.PermManageShares: true,
			},
		},
		{name: "unknown role", role: task.Role("bogus"), want: nil},
		{name: "empty role", role: task.Role(""), want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, p := range allPerms {
				if got := tt.role.Can(p); got != tt.want[p] {
					t.Errorf("Can(%#b) = %t, want %t", p, got, tt.want[p])
				}
			}
		})
	}
}

func TestRoleCanCombinedPermissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		role task.Role
		perm task.Permission
		want bool
	}{
		{"all required held", task.RoleEditor, task.PermChangeStatus | task.PermEditContent, true},
		{"one of several missing", task.RoleContributor, task.PermChangeStatus | task.PermEditContent, false},
		{"none held", task.RoleViewer, task.PermDelete | task.PermAssign, false},
		{"no permission required", task.RoleViewer, 0, true},
		{"no permission required for unknown role", task.Role("bogus"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.Can(tt.perm); got != tt.want {
				t.Errorf("Can(%#b) = %t, want %t", tt.perm, got, tt.want)
			}
		})
	}
}

func TestRolePermissionsGrowWithRole(t *testing.T) {
	t.Parallel()
	// Each role in the ladder must hold everything the one below it holds.
	ladder := []task.Role{
		task.RoleViewer,
		task.RoleContributor,
		task.RoleEditor,
		task.RoleManager,
		task.RoleOwner,
	}
	perms := []task.Permission{
		task.PermView,
		task.PermViewShares,
		task.PermChangeStatus,
		task.PermEditContent,
		task.PermAssign,
		task.PermDelete,
		task.PermManageShares,
	}

	for i := 1; i < len(ladder); i++ {
		lower, higher := ladder[i-1], ladder[i]
		t.Run(string(higher), func(t *testing.T) {
			for _, p := range perms {
				if lower.Can(p) && !higher.Can(p) {
					t.Errorf("%s can %#b but %s cannot", lower, p, higher)
				}
			}
		})
	}
}

func TestRoleIsGrantable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role task.Role
		want bool
	}{
		{task.RoleManager, true},
		{task.RoleEditor, true},
		{task.RoleContributor, true},
		{task.RoleViewer, true},
		{task.RoleOwner, false},
		{task.Role("bogus"), false},
		{task.Role(""), false},
		{task.Role("Viewer"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := tt.role.IsGrantable(); got != tt.want {
				t.Errorf("IsGrantable() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRequiredForPatch(t *testing.T) {
	t.Parallel()
	title := "title"
	desc := "description"
	status := task.StatusDone
	due := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		patch task.Patch
		want  task.Permission
	}{
		{"empty patch", task.Patch{}, 0},
		{"title", task.Patch{Title: &title}, task.PermEditContent},
		{"description", task.Patch{Description: &desc}, task.PermEditContent},
		{"due date", task.Patch{DueAt: &due}, task.PermEditContent},
		{"status", task.Patch{Status: &status}, task.PermChangeStatus},
		{
			"all content fields",
			task.Patch{Title: &title, Description: &desc, DueAt: &due},
			task.PermEditContent,
		},
		{
			"content and status",
			task.Patch{Title: &title, Status: &status},
			task.PermEditContent | task.PermChangeStatus,
		},
		{
			"every field",
			task.Patch{Title: &title, Description: &desc, Status: &status, DueAt: &due},
			task.PermEditContent | task.PermChangeStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := task.RequiredForPatch(tt.patch); got != tt.want {
				t.Errorf("RequiredForPatch() = %#b, want %#b", got, tt.want)
			}
		})
	}
}

func TestRequiredForPatchAgainstRoles(t *testing.T) {
	t.Parallel()
	title := "title"
	status := task.StatusDoing

	tests := []struct {
		name  string
		patch task.Patch
		role  task.Role
		want  bool
	}{
		{"viewer cannot change status", task.Patch{Status: &status}, task.RoleViewer, false},
		{"contributor can change status", task.Patch{Status: &status}, task.RoleContributor, true},
		{"contributor cannot edit content", task.Patch{Title: &title}, task.RoleContributor, false},
		{"contributor cannot do a mixed patch", task.Patch{Title: &title, Status: &status}, task.RoleContributor, false},
		{"editor can do a mixed patch", task.Patch{Title: &title, Status: &status}, task.RoleEditor, true},
		{"anyone can apply an empty patch", task.Patch{}, task.RoleViewer, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.Can(task.RequiredForPatch(tt.patch)); got != tt.want {
				t.Errorf("Can(RequiredForPatch()) = %t, want %t", got, tt.want)
			}
		})
	}
}
