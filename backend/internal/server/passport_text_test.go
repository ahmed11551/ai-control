package server

import "testing"

func TestLooksLikePassportText(t *testing.T) {
	yes := []string{
		"Паспорт 4510 123456 выдан ОВД",
		"серия 45 10 номер 123456",
		"Паспортные данные:\nсерия 4012 № 654321\nкод подразделения 770-001",
		"ФИО Иванов\nпаспорт 4010 998877\nвыдан 12.03.2015",
	}
	no := []string{
		"привет",
		"нужен паспорт",
		"скинь паспорт плиз",
		"паспорт готов?",
		"12345",
		"",
	}
	for _, s := range yes {
		if !looksLikePassportText(s) {
			t.Fatalf("expected passport text: %q", s)
		}
	}
	for _, s := range no {
		if looksLikePassportText(s) {
			t.Fatalf("expected NOT passport text: %q", s)
		}
	}
}
