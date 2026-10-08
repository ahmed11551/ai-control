package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStaffMediaAbsPathRejectsTraversal(t *testing.T) {
	a := &App{}
	_, err := a.staffMediaAbsPath("../etc/passwd")
	if err == nil {
		t.Fatal("expected path traversal to fail")
	}
	key := "dialog-id/out-1-photo.jpg"
	abs, err := a.staffMediaAbsPath(key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(a.staffDialogMediaRoot())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		t.Fatalf("abs %q not under root %q", abs, root)
	}
	if !a.staffMediaFileExists("") {
		// empty key must be missing
	} else {
		t.Fatal("empty key should not exist")
	}
}
