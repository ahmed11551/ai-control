package server

import (
	"strings"
	"testing"
)

func TestParseMskTowerIntake(t *testing.T) {
	in, err := parseMskTowerIntake(`Номер: +79885747292
Имя: Сергей 
Возраст: 22
Город: Ростовская область 
Доп инф: ранее обращался в другую организацию`)
	if err != nil {
		t.Fatal(err)
	}
	if in.Phone != "79885747292" || in.Name != "Сергей" || in.Age != "22" || in.City != "Ростовская область" {
		t.Fatalf("%+v", in)
	}
	if !strings.Contains(in.Extra, "Доп инф: ранее обращался в другую организацию") {
		t.Fatalf("extra %q", in.Extra)
	}
	note := mskTowerCardNote(in.Age, in.Extra)
	if !strings.Contains(note, "Возраст: 22") || !strings.Contains(note, "ранее обращался") {
		t.Fatalf("note %q", note)
	}
}

func TestParseMskTowerIntakeRequiresNameAndPhone(t *testing.T) {
	if _, err := parseMskTowerIntake("Имя: Сергей"); err == nil {
		t.Fatal("phone is required")
	}
	if _, err := parseMskTowerIntake("Номер: +79885747292"); err == nil {
		t.Fatal("name is required")
	}
	if _, err := parseMskTowerIntake("Номер: 123\nИмя: Сергей"); err == nil {
		t.Fatal("short number must be rejected")
	}
	in, err := parseMskTowerIntake("Телефон: 89885747292\nИмя: Сергей\nКомментарий: созвон вечером")
	if err != nil {
		t.Fatal(err)
	}
	if in.Phone != "79885747292" || in.Extra != "созвон вечером" {
		t.Fatalf("%+v", in)
	}
}

func TestPickMskTowerDepartment(t *testing.T) {
	got, err := pickMskTowerDepartment([]towerDept{
		{ID: "ivan", Name: "Отдел Ивана", Head: "h1"},
		{ID: "bare", Name: "Башня", Head: "h2"},
		{ID: "tower", Name: "МСК Башня", Head: "rop"},
	})
	if err != nil || got.ID != "tower" || got.Head != "rop" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := pickMskTowerDepartment([]towerDept{{ID: "a", Name: "МСК", Head: "h"}}); err != errMskTowerDeptMissing {
		t.Fatalf("err %v", err)
	}
	if _, err := pickMskTowerDepartment([]towerDept{{ID: "a", Name: "МСК Башня", Head: ""}}); err != errMskTowerNoROP {
		t.Fatalf("err %v", err)
	}
	if _, err := pickMskTowerDepartment([]towerDept{
		{ID: "a", Name: "МСК Башня", Head: "h1"},
		{ID: "b", Name: "Отдел МСК Башня", Head: "h2"},
	}); err != errMskTowerDeptAmbiguous {
		t.Fatalf("err %v", err)
	}
}

func TestTelegramUserAllowed(t *testing.T) {
	if telegramUserAllowed("", 42) || telegramUserAllowed("1,2", 0) {
		t.Fatal("empty list and zero id must be denied")
	}
	if !telegramUserAllowed(" 42 , 7 ", 42) {
		t.Fatal("listed operator must be allowed")
	}
	if telegramUserAllowed("42", 43) {
		t.Fatal("other user must be denied")
	}
}

func TestMskTowerBotRefusesSharedToken(t *testing.T) {
	a := &App{}
	a.cfg.TelegramBotToken = "same"
	a.cfg.TelegramMskTowerBotToken = "same"
	if a.mskTowerBotReady() {
		t.Fatal("tower bot must not share the ticket bot token")
	}
	a.cfg.TelegramMskTowerBotToken = "other"
	if !a.mskTowerBotReady() {
		t.Fatal("a separate token must start the tower bot")
	}
}
