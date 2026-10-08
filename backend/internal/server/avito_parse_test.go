package server

import "testing"

func TestExtractAvitoIDsFromObject(t *testing.T) {
	raw := map[string]any{"id": "11102026de0ad1be10e2236f"}
	got := extractAvitoIDs(raw)
	if len(got) != 1 || got[0] != "11102026de0ad1be10e2236f" {
		t.Fatalf("%v", got)
	}
}

func TestExtractAvitoIDsFromList(t *testing.T) {
	raw := map[string]any{
		"ids": []any{"a1", "a2", "a1"},
	}
	got := extractAvitoIDs(raw)
	if len(got) != 2 || got[0] != "a1" || got[1] != "a2" {
		t.Fatalf("%v", got)
	}
}

func TestExtractAvitoIDsNestedPayload(t *testing.T) {
	raw := map[string]any{
		"payload": map[string]any{
			"applies": []any{
				map[string]any{"id": "n1"},
				map[string]any{"application_id": "n2"},
			},
		},
	}
	got := extractAvitoIDs(raw)
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestAvitoLeadTitle(t *testing.T) {
	if avitoLeadTitle("Иван", "1872", "x") != "Иван · Avito 1872" {
		t.Fatal("name+vacancy")
	}
	if avitoLeadTitle("", "", "abc") != "Avito отклик abc" {
		t.Fatal("id only")
	}
}

func TestParseAvitoApply(t *testing.T) {
	m := map[string]any{
		"id":         "app-1",
		"vacancy_id": 18725423,
		"applicant": map[string]any{
			"data": map[string]any{
				"name": "Иванов Иван",
				"full_name": map[string]any{
					"first_name": "Иван",
					"last_name":  "Иванов",
				},
			},
		},
		"contacts": map[string]any{
			"phones": []any{
				map[string]any{"status": "requested", "value": "70000000000"},
				map[string]any{"status": "received", "value": "72002000014"},
			},
		},
	}
	got := parseAvitoApply(m)
	if got.ID != "app-1" || got.Name != "Иванов Иван" || got.Phone != "72002000014" || got.Vacancy != "18725423" {
		t.Fatalf("%+v", got)
	}
}
