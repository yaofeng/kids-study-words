package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxImportBytes = 10 << 20

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.renderSettingsError(w, r, http.StatusRequestEntityTooLarge, "文件太大，最多 10 MB。")
			return
		}
		s.renderSettingsError(w, r, http.StatusBadRequest, "表单格式不对，请重新选择文件。")
		return
	}
	defer r.MultipartForm.RemoveAll()

	mode := r.PostFormValue("mode")
	var importMode store.ImportMode
	switch mode {
	case "merge":
		importMode = store.ImportMerge
	case "overwrite":
		importMode = store.ImportOverwrite
		if r.PostFormValue("confirm") != "yes" {
			s.renderError(w, r, http.StatusBadRequest, "覆盖导入需要先勾选确认。")
			return
		}
	default:
		s.renderError(w, r, http.StatusBadRequest, "请选择合并或覆盖。")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		s.renderSettingsError(w, r, http.StatusBadRequest, "请选择一个导出文件。")
		return
	}
	defer file.Close()

	var doc store.ExportFile
	dec := json.NewDecoder(io.LimitReader(file, maxImportBytes))
	if err := dec.Decode(&doc); err != nil {
		s.renderSettingsError(w, r, http.StatusOK, "这个文件读不出来，请选择「少儿背单词」导出的 JSON 文件。")
		return
	}
	if err := store.ValidateExport(doc); err != nil {
		s.renderSettingsError(w, r, http.StatusOK, "导入失败："+err.Error())
		return
	}

	stats, err := s.store.Import(r.Context(), currentUser(r).ID, doc, importMode)
	if err != nil {
		slog.Warn("import failed", "err", err)
		s.renderSettingsError(w, r, http.StatusOK, "导入失败："+err.Error())
		return
	}
	setFlash(w, importSummary(stats, importMode))
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// importSummary turns stats into the flash message after an import.
func importSummary(stats store.ImportStats, mode store.ImportMode) string {
	var parts []string
	if mode == store.ImportOverwrite {
		parts = append(parts, "已覆盖原有数据")
	}
	if stats.NotebooksCreated > 0 {
		parts = append(parts, fmt.Sprintf("新建 %d 个生词本", stats.NotebooksCreated))
	}
	if stats.WordsCreated > 0 {
		parts = append(parts, fmt.Sprintf("新增 %d 个单词", stats.WordsCreated))
	}
	if stats.WordsUpdated > 0 {
		parts = append(parts, fmt.Sprintf("更新 %d 个单词", stats.WordsUpdated))
	}
	if stats.GroupsCreated > 0 {
		parts = append(parts, fmt.Sprintf("新建 %d 个分组", stats.GroupsCreated))
	}
	if stats.GroupMembershipsAdded > 0 {
		parts = append(parts, fmt.Sprintf("恢复 %d 个分组词", stats.GroupMembershipsAdded))
	}
	if stats.LogsAdded > 0 {
		parts = append(parts, fmt.Sprintf("恢复 %d 条回忆记录", stats.LogsAdded))
	}
	if len(parts) == 0 {
		return "导入完成，没有需要变更的内容。"
	}
	return "导入完成：" + strings.Join(parts, "，") + "。"
}
