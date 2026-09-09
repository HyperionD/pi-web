package piweb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	maxUserImportBytes = 5 << 20
	maxUserImportRows  = 1000
)

var userImportHeaders = []string{"账号", "用户名", "部门", "初始密码", "工作区路径", "禁用"}

type accountImportRow struct {
	rowNumber      int
	account        string
	username       string
	department     string
	password       string
	workspacePaths []string
	disabled       bool
}

func (s *Server) userImportTemplate(w http.ResponseWriter, _ *http.Request) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	sheet := file.GetSheetName(0)
	if err := file.SetSheetName(sheet, "用户导入"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	headers := make([]interface{}, len(userImportHeaders))
	for index, header := range userImportHeaders {
		headers[index] = header
	}
	if err := file.SetSheetRow("用户导入", "A1", &headers); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var output bytes.Buffer
	if err := file.Write(&output); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="users-import-template.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func (s *Server) importAccounts(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUserImportBytes+1)
	if err := r.ParseMultipartForm(maxUserImportBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("导入文件不能超过 5 MiB"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("请选择 .xlsx 导入文件"))
		return
	}
	defer file.Close()
	if strings.ToLower(filepath.Ext(header.Filename)) != ".xlsx" {
		writeError(w, http.StatusBadRequest, errors.New("只支持 .xlsx 文件"))
		return
	}
	rows, err := parseUserImportRows(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	imported, err := s.saveImportedAccounts(rows)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": imported})
}

func parseUserImportRows(reader io.Reader) ([]accountImportRow, error) {
	file, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, errors.New("无法读取 Excel 文件")
	}
	defer func() { _ = file.Close() }()
	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("Excel 文件没有工作表")
	}
	rows, err := file.GetRows(sheets[0])
	if err != nil {
		return nil, errors.New("无法读取 Excel 工作表")
	}
	if len(rows) == 0 || !validUserImportHeader(rows[0]) {
		return nil, fmt.Errorf("模板表头必须为：%s", strings.Join(userImportHeaders, "、"))
	}
	parsed := make([]accountImportRow, 0, len(rows)-1)
	for index, row := range rows[1:] {
		rowNumber := index + 2
		if emptyImportRow(row) {
			continue
		}
		if len(parsed) >= maxUserImportRows {
			return nil, fmt.Errorf("导入用户不能超过 %d 条", maxUserImportRows)
		}
		parsedRow, err := parseUserImportRow(row, rowNumber)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, parsedRow)
	}
	if len(parsed) == 0 {
		return nil, errors.New("Excel 中没有可导入的用户")
	}
	return parsed, nil
}

func validUserImportHeader(row []string) bool {
	if len(row) < len(userImportHeaders) {
		return false
	}
	for index, header := range userImportHeaders {
		if strings.TrimSpace(row[index]) != header {
			return false
		}
	}
	return true
}

func emptyImportRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseUserImportRow(row []string, rowNumber int) (accountImportRow, error) {
	account := strings.TrimSpace(importCell(row, 0))
	username := strings.TrimSpace(importCell(row, 1))
	department := strings.TrimSpace(importCell(row, 2))
	password := importCell(row, 3)
	if account == "" || username == "" || password == "" {
		return accountImportRow{}, fmt.Errorf("第 %d 行：账号、用户名、初始密码不能为空", rowNumber)
	}
	if !accountName.MatchString(account) {
		return accountImportRow{}, fmt.Errorf("第 %d 行：账号格式无效", rowNumber)
	}
	if err := validateAdminPassword(password); err != nil {
		return accountImportRow{}, fmt.Errorf("第 %d 行：初始密码无效", rowNumber)
	}
	disabled, err := parseImportDisabled(importCell(row, 5))
	if err != nil {
		return accountImportRow{}, fmt.Errorf("第 %d 行：%w", rowNumber, err)
	}
	return accountImportRow{
		rowNumber: rowNumber, account: account, username: username, department: department,
		password: password, workspacePaths: splitImportPaths(importCell(row, 4)), disabled: disabled,
	}, nil
}

func importCell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return row[index]
}

func splitImportPaths(value string) []string {
	parts := strings.Split(value, ";")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func parseImportDisabled(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "否", "false", "0", "no":
		return false, nil
	case "是", "true", "1", "yes":
		return true, nil
	default:
		return false, errors.New("禁用列只能填写是/否")
	}
}

func (s *Server) saveImportedAccounts(rows []accountImportRow) (int, error) {
	s.accounts.mu.Lock()
	defer s.accounts.mu.Unlock()
	if err := validateImportedAccounts(rows, s.accounts.Users); err != nil {
		return 0, err
	}
	hashes := make(map[int]string, len(rows))
	for _, row := range rows {
		hash, err := hashAdminPassword(row.password)
		if err != nil {
			return 0, fmt.Errorf("第 %d 行：无法生成密码哈希", row.rowNumber)
		}
		hashes[row.rowNumber] = hash
	}
	users := mapsCloneAccounts(s.accounts.Users)
	for _, row := range rows {
		paths, err := s.accountPaths(nil, row.workspacePaths)
		if err != nil {
			return 0, fmt.Errorf("第 %d 行：工作区路径无效", row.rowNumber)
		}
		ids := make([]string, 0, len(paths))
		for id := range paths {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		users[row.account] = accountRecord{
			Account:      Account{ID: accountID(), Account: row.account, Username: row.username, Department: row.department, Role: "user", Disabled: row.disabled, Workspaces: ids},
			PasswordHash: hashes[row.rowNumber], Paths: paths,
		}
	}
	if err := s.accounts.save(users, s.accounts.Owners); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func validateImportedAccounts(rows []accountImportRow, existing map[string]accountRecord) error {
	seen := make(map[string]int, len(existing)+len(rows))
	for account := range existing {
		seen[strings.ToLower(strings.TrimSpace(account))] = 0
	}
	for _, row := range rows {
		key := strings.ToLower(row.account)
		if previous, ok := seen[key]; ok {
			if previous == 0 {
				return fmt.Errorf("第 %d 行：账号 %q 已存在", row.rowNumber, row.account)
			}
			return fmt.Errorf("第 %d 行：账号 %q 与第 %d 行重复", row.rowNumber, row.account, previous)
		}
		seen[key] = row.rowNumber
	}
	return nil
}

func mapsCloneAccounts(source map[string]accountRecord) map[string]accountRecord {
	cloned := make(map[string]accountRecord, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
