package server

import "testing"

func TestSupportRoleOK(t *testing.T) {
	for _, role := range []string{"admin", "rop", "manager", "sales", "ROP"} {
		if !supportRoleOK(role) {
			t.Fatalf("role %q must be allowed", role)
		}
	}
	for _, role := range []string{"logist", "night_logist", "guest", ""} {
		if supportRoleOK(role) {
			t.Fatalf("role %q must be denied", role)
		}
	}
}
