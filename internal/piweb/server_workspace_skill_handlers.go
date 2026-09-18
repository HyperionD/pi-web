package piweb

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) workspaceSkills(w http.ResponseWriter, r *http.Request) {
	if !s.config.EnablePiExecution {
		writeJSON(w, http.StatusOK, SkillDiscoveryResponse{
			Skills: []Skill{}, Available: false, Error: "Pi execution is disabled",
		})
		return
	}
	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	skills, err := loadWorkspaceSkills(r.Context(), root)
	if err != nil {
		writeSkillDiscoveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SkillDiscoveryResponse{
		Skills: skills, Available: true,
	})
}

func (s *Server) patchWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	var request SkillPatchRequest
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(request.Path) == "" || request.Enabled == nil {
		writeError(w, http.StatusBadRequest, errors.New("skill path and enabled are required"))
		return
	}
	if !s.config.EnablePiExecution {
		writeError(w, http.StatusServiceUnavailable, errors.New("Pi execution is disabled"))
		return
	}

	s.skillMutationMu.Lock()
	defer s.skillMutationMu.Unlock()

	root, err := s.store.WorkspacePath(r.PathValue("workspaceID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	skills, err := loadWorkspaceSkills(r.Context(), root)
	if err != nil {
		writeSkillDiscoveryError(w, err)
		return
	}
	requestedPath, err := canonicalSkillPath(request.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("skill was not found"))
		return
	}
	selected, ok := findSkillByPath(skills, requestedPath)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("skill was not found"))
		return
	}
	if actor := requestActor(r); actor != nil && actor.Role != "admin" && selected.Scope != "project" {
		writeError(w, http.StatusForbidden, errAccountDenied)
		return
	}
	if selected.Scope == "project" {
		policy, policyErr := s.workspacePolicy()
		if policyErr != nil {
			writeWorkspacePolicyError(w, policyErr)
			return
		}
		if policyErr := policy.CheckExistingDirectory(filepath.Dir(selected.Path)); policyErr != nil {
			writeWorkspacePolicyError(w, policyErr)
			return
		}
	}
	if err := updateSkillFile(selected.Path, *request.Enabled); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusConflict, errors.New("skill changed while updating"))
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}

	updated, err := loadWorkspaceSkills(r.Context(), root)
	if err != nil {
		writeSkillDiscoveryError(w, err)
		return
	}
	selected, ok = findSkillByPath(updated, requestedPath)
	if !ok {
		writeError(w, http.StatusConflict, errors.New("skill changed while updating"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]Skill{"skill": selected})
}

func updateSkillFile(path string, enabled bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := setSkillInvocation(content, enabled)
	if err != nil {
		return err
	}
	if string(updated) == string(content) {
		return nil
	}
	return writeSkillFileAtomic(path, updated)
}

func findSkillByPath(skills []Skill, path string) (Skill, bool) {
	for _, skill := range skills {
		if skill.Path == path {
			return skill, true
		}
	}
	return Skill{}, false
}

func writeSkillDiscoveryError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		status = http.StatusGatewayTimeout
	}
	writeError(w, status, err)
}
