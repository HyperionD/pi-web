package piweb

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func SafeJoin(root, rel string) (string, error) {
	root = filepath.Clean(root)
	cleanRel := filepath.Clean(strings.TrimPrefix(rel, "/"))
	if cleanRel == "." || cleanRel == "" {
		return "", errors.New("path is required")
	}
	full := filepath.Join(root, cleanRel)
	relBack, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relBack, "..") || filepath.IsAbs(relBack) {
		return "", errors.New("path traversal is not allowed")
	}
	return full, nil
}

type GitFileDiffResult struct {
	Supported bool   `json:"supported"`
	Status    string `json:"status"`
	Patch     string `json:"patch,omitempty"`
}

func GitFileDiff(root, rel string) (GitFileDiffResult, error) {
	cleanRel, err := safeGitRelativePath(root, rel)
	if err != nil {
		return GitFileDiffResult{}, err
	}
	if _, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); err != nil {
		return GitFileDiffResult{Supported: false, Status: "clean"}, nil
	}
	statusBytes, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--", cleanRel).Output()
	if err != nil {
		return GitFileDiffResult{}, err
	}
	status := "clean"
	if len(statusBytes) >= 2 {
		status = gitStatusKind(string(statusBytes[:2]))
	}
	if status == "clean" {
		return GitFileDiffResult{Supported: true, Status: status}, nil
	}
	var patch []byte
	if status == "untracked" {
		full, err := SafeJoin(root, cleanRel)
		if err != nil {
			return GitFileDiffResult{}, err
		}
		patch, err = gitDiffCommand(root, "--no-index", "--", "/dev/null", full)
	} else {
		patch, err = gitDiffCommand(root, "HEAD", "--", cleanRel)
	}
	if err != nil {
		return GitFileDiffResult{}, err
	}
	return GitFileDiffResult{Supported: true, Status: status, Patch: string(patch)}, nil
}

func safeGitRelativePath(root, rel string) (string, error) {
	full, err := SafeJoin(root, rel)
	if err != nil {
		return "", err
	}
	cleanRel, err := filepath.Rel(filepath.Clean(root), full)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(cleanRel), nil
}

func gitDiffCommand(root string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", root, "diff", "--no-ext-diff", "--no-color"}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err == nil {
		return output, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return output, nil
	}
	return nil, err
}

func GitOriginalFileContent(root, rel string) (string, string) {
	cleanRel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(rel, "/")))
	if cleanRel == "." || cleanRel == "" {
		return "", ""
	}
	trackedPathBytes, err := exec.Command("git", "-C", root, "ls-files", "--full-name", "--", cleanRel).Output()
	if err != nil {
		return "", ""
	}
	trackedPath := strings.TrimSpace(string(trackedPathBytes))
	if trackedPath == "" {
		if _, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); err == nil {
			return "", "untracked"
		}
		return "", ""
	}
	blob, err := exec.Command("git", "-C", root, "show", "HEAD:"+trackedPath).Output()
	if err != nil {
		return "", "added"
	}
	return string(blob), "tracked"
}

func RealGitStatus(root string) (GitStatus, error) {
	branchBytes, err := exec.Command("git", "-C", root, "branch", "--show-current").Output()
	if err != nil {
		return GitStatus{}, err
	}
	statusBytes, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z").Output()
	if err != nil {
		return GitStatus{}, err
	}
	branch := strings.TrimSpace(string(branchBytes))
	if branch == "" {
		branch = "HEAD"
	}
	files := ParseGitStatusPorcelain(statusBytes)
	return GitStatus{Branch: branch, Dirty: len(files), Files: files}, nil
}

func ParseGitStatusPorcelain(output []byte) map[string]string {
	files := map[string]string{}
	fields := strings.Split(string(output), "\x00")
	for index := 0; index < len(fields); index++ {
		entry := fields[index]
		if len(entry) < 4 {
			continue
		}
		code := entry[:2]
		path := filepath.ToSlash(strings.TrimSpace(entry[3:]))
		if path == "" {
			continue
		}
		status := gitStatusKind(code)
		files[path] = status
		if code[0] == 'R' || code[1] == 'R' || code[0] == 'C' || code[1] == 'C' {
			if index+1 < len(fields) && fields[index+1] != "" {
				oldPath := filepath.ToSlash(strings.TrimSpace(fields[index+1]))
				if oldPath != "" {
					files[oldPath] = "deleted"
				}
				index++
			}
		}
	}
	return files
}

func gitStatusKind(code string) string {
	if code == "??" {
		return "untracked"
	}
	if strings.ContainsAny(code, "R") || strings.ContainsAny(code, "C") {
		return "renamed"
	}
	if strings.ContainsAny(code, "D") {
		return "deleted"
	}
	if strings.ContainsAny(code, "A") {
		return "added"
	}
	if strings.ContainsAny(code, "MUT") {
		return "modified"
	}
	return "modified"
}

func SessionShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func uniqueID(base string, used map[string]int) string {
	if used[base] == 0 {
		used[base] = 1
		return base
	}
	used[base]++
	return base + "-" + strconv.Itoa(used[base])
}
