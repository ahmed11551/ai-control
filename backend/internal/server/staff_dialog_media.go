package server

import (
	"archive/zip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (a *App) staffDialogMediaRoot() string {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	return filepath.Join(root, "data", "staff-dialog-media")
}

func (a *App) staffMediaAbsPath(storageKey string) (string, error) {
	root, err := filepath.Abs(a.staffDialogMediaRoot())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(a.staffDialogMediaRoot(), filepath.FromSlash(storageKey)))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("bad path")
	}
	return abs, nil
}

func (a *App) staffMediaFileExists(storageKey string) bool {
	if strings.TrimSpace(storageKey) == "" {
		return false
	}
	abs, err := a.staffMediaAbsPath(storageKey)
	if err != nil {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && !st.IsDir()
}

func (a *App) getStaffDialogMessageMedia(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	msgID := chi.URLParam(r, "id")
	var dialogID string
	err := a.db.QueryRow(r.Context(), `
		SELECT dialog_id::text FROM staff_dialog_messages WHERE id=$1::uuid`, msgID).Scan(&dialogID)
	if err != nil || dialogID == "" {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if hideIfMissing(w, a.canAccessStaffDialog(r.Context(), r, dialogID)) {
		return
	}

	files := a.queryMaps(r.Context(), `
		SELECT id::text, kind, filename, mime, size_bytes, storage_key
		FROM staff_dialog_message_files
		WHERE message_id=$1::uuid
		ORDER BY sort_order, created_at`, msgID)
	var ready []map[string]any
	for _, f := range files {
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		if a.staffMediaFileExists(key) {
			ready = append(ready, f)
		}
	}
	if len(ready) == 0 {
		writeJSON(w, 404, map[string]string{"error": "файл не найден на диске"})
		return
	}

	if len(ready) == 1 {
		f := ready[0]
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		abs, err := a.staffMediaAbsPath(key)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "файл не найден"})
			return
		}
		filename := sanitizeMediaFilename(fmt.Sprint(f["filename"]))
		mime := strings.TrimSpace(fmt.Sprint(f["mime"]))
		if mime == "" {
			mime = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
		http.ServeFile(w, r, abs)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="attachments.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	used := map[string]int{}
	for _, f := range ready {
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		abs, err := a.staffMediaAbsPath(key)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		name := sanitizeMediaFilename(fmt.Sprint(f["filename"]))
		if n := used[name]; n > 0 {
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s_%d%s", base, n+1, ext)
		}
		used[sanitizeMediaFilename(fmt.Sprint(f["filename"]))]++
		fw, err := zw.Create(name)
		if err != nil {
			continue
		}
		_, _ = fw.Write(raw)
	}
}
