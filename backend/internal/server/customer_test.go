package server

import (
	"strings"
	"testing"
)

func TestCustomerSenderIncludesAdmin(t *testing.T) {
	for _, role := range []string{"admin", "Admin", "logist", "night_logist"} {
		if !customerSenderOrAdmin(role) {
			t.Fatalf("%s must send", role)
		}
	}
	for _, role := range []string{"manager", "customer", "rop", ""} {
		if customerSenderOrAdmin(role) {
			t.Fatalf("%s must not send", role)
		}
	}
}

func TestCustomerAPIAllowed(t *testing.T) {
	if !customerAPIAllowed("/me") || !customerAPIAllowed("/customer") || !customerAPIAllowed("/customer/people") {
		t.Fatal("cabinet paths must stay open")
	}
	if !customerAPIAllowed("/customer/people/x/files/y") {
		t.Fatal("file path")
	}
	for _, path := range []string{"/deals", "/customerish", "/customers", "/tasks", ""} {
		if customerAPIAllowed(path) {
			t.Fatalf("%q must stay closed", path)
		}
	}
}

func TestValidateCustomerForm(t *testing.T) {
	form, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "4510 123456", "2026-10-12", "Охранник")
	if msg != "" || form.FullName != "Иванов Иван" || form.Passport != "4510 123456" || form.Arrival != "2026-10-12" {
		t.Fatalf("%+v %q", form, msg)
	}
	if _, msg := validateCustomerForm("И", "1990-02-01", "4510 123456", "2026-10-12", "Охранник"); msg == "" {
		t.Fatal("short name")
	}
	if _, msg := validateCustomerForm("Иванов Иван", "2999-01-01", "4510 123456", "2026-10-12", "Охранник"); msg == "" {
		t.Fatal("future birth")
	}
	if _, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "абв", "2026-10-12", "Охранник"); msg == "" {
		t.Fatal("passport letters")
	}
	if _, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "4510", "2026-10-12", "Охранник"); msg == "" {
		t.Fatal("series only")
	}
	glued, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "4510123456", "2026-10-12", "Охранник")
	if msg != "" || glued.Passport != "4510 123456" {
		t.Fatalf("glued passport %+v %q", glued, msg)
	}
	if _, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "4510 123456", "вчера", "Охранник"); msg == "" {
		t.Fatal("bad arrival")
	}
	if _, msg := validateCustomerForm("Иванов Иван", "1990-02-01", "4510 123456", "2026-10-12", "  "); msg == "" {
		t.Fatal("empty position")
	}
}

func TestDecodeCustomerFiles(t *testing.T) {
	raw := []map[string]any{{"id": "1", "filename": "Паспорт.svg"}}
	got := decodeCustomerFiles(raw)
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	if decodeCustomerFiles("[]") != nil && len(decodeCustomerFiles("[]")) != 0 {
		t.Fatal("empty")
	}
}

func TestCustomerSiteByCode(t *testing.T) {
	site, ok := customerSiteByCode("saransk")
	if !ok || site.Name != "Саранск" || site.Login != "zakazchik.saransk" {
		t.Fatalf("%+v %v", site, ok)
	}
	if _, ok := customerSiteByCode("paris"); ok {
		t.Fatal("unknown site")
	}
	if len(customerSiteCatalog()) != 4 {
		t.Fatal("four customers")
	}
}

func TestCustomerPeopleFilterSite(t *testing.T) {
	where, args := customerPeopleFilter("", "", "", "", "", "", "", "")
	if strings.Contains(where, "site_id") || strings.Contains(where, "department_id") || len(args) != 0 {
		t.Fatalf("all sites: %s %v", where, args)
	}
	where, args = customerPeopleFilter("site-1", "", "", "", "", "", "", "")
	if !strings.Contains(where, "s.site_id = $1::uuid") || len(args) != 1 || args[0] != "site-1" {
		t.Fatalf("one site: %s %v", where, args)
	}
	where, args = customerPeopleFilter("", "none", "", "", "", "", "", "")
	if !strings.Contains(where, "department_id IS NOT NULL") || len(args) != 0 {
		t.Fatalf("no department: %s %v", where, args)
	}
	id := "11111111-1111-4111-8111-111111111111"
	where, args = customerPeopleFilter("", id, "", "", "", "", "", "")
	if !strings.Contains(where, "ou.department_id = $1::uuid") || len(args) != 1 || args[0] != id {
		t.Fatalf("one department: %s %v", where, args)
	}
	if cleanDeptFilter("not-a-uuid") != "" || cleanDeptFilter("none") != "none" {
		t.Fatal("dept filter")
	}
}

func TestCustomerPeoplePage(t *testing.T) {
	if customerPeoplePage("") != 1 || customerPeoplePage("0") != 1 || customerPeoplePage("x") != 1 {
		t.Fatal("bad page falls back to 1")
	}
	if customerPeoplePage("3") != 3 {
		t.Fatal("page 3")
	}
	if customerPeoplePage("999999") != 10000 {
		t.Fatal("page cap")
	}
	if customerPeoplePageSize != 20 {
		t.Fatal("page size")
	}
}
