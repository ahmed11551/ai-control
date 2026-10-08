package server

import "testing"

func TestNormalizeOfficeCIDR(t *testing.T) {
	if s, ok := normalizeOfficeCIDR(" 203.0.113.10 "); !ok || s != "203.0.113.10" {
		t.Fatalf("%q %v", s, ok)
	}
	if s, ok := normalizeOfficeCIDR("203.0.113.0/24"); !ok || s != "203.0.113.0/24" {
		t.Fatalf("%q %v", s, ok)
	}
	if _, ok := normalizeOfficeCIDR("not-an-ip"); ok {
		t.Fatal("garbage accepted")
	}
}

func TestIPInOfficeList(t *testing.T) {
	list := []string{"203.0.113.10", "198.51.100.0/24"}
	if !ipInOfficeList("203.0.113.10", list) {
		t.Fatal("exact")
	}
	if !ipInOfficeList("198.51.100.15", list) {
		t.Fatal("cidr")
	}
	if ipInOfficeList("198.51.100.15", nil) {
		t.Fatal("empty must not match")
	}
	if ipInOfficeList("8.8.8.8", list) {
		t.Fatal("foreign")
	}
}

func TestOfficeIPExempt(t *testing.T) {
	if !officeIPExempt("admin") || !officeIPExempt("logist") || !officeIPExempt(" Logist ") || !officeIPExempt("night_logist") || !officeIPExempt(" Night_Logist ") || !officeIPExempt("customer") {
		t.Fatal("admin, both logists and customer skip the office IP gate")
	}
	for _, role := range []string{"manager", "rop", "sales", ""} {
		if officeIPExempt(role) {
			t.Fatalf("%q must stay behind the office IP gate", role)
		}
	}
}
