package piweb

import (
	"context"
	"errors"
	"strings"
)

type ModelSelection struct {
	Source          string `json:"source,omitempty"`
	InvalidPersonal bool   `json:"invalidPersonal,omitempty"`
	CatalogError    string `json:"catalogError,omitempty"`
}

func modelPair(values map[string]any) (ModelStartupSettings, bool) {
	provider, _ := values["defaultProvider"].(string)
	model, _ := values["defaultModel"].(string)
	return ModelStartupSettings{Provider: provider, Model: model}, provider != "" && model != ""
}

func resolveModelSelection(catalog WorkspaceModelsResponse, global, personal map[string]any) (ModelStartupSettings, bool) {
	selected, complete := modelPair(personal)
	hasPersonal := selected.Provider != "" || selected.Model != ""
	if complete && catalogContains(catalog, selected.Provider, selected.Model) {
		return selected, false
	}
	admin, hasAdmin := modelPair(global)
	if hasAdmin && catalogContains(catalog, admin.Provider, admin.Model) {
		return admin, hasPersonal
	}
	first := catalog.Providers[0].Models[0]
	return ModelStartupSettings{Provider: first.Provider, Model: first.ID}, hasPersonal
}

func (s *Server) accountWorkspaceSettings(root, workspaceID, userID string) (WorkspaceSettingsResponse, error) {
	settings, err := WorkspaceSettingsForUser(root, workspaceID, userID)
	if err != nil {
		return WorkspaceSettingsResponse{}, err
	}
	catalog := fallbackWorkspaceModels(root, nil)
	if s.config.EnablePiExecution {
		catalog, err = configuredModels(context.Background(), root)
	}
	if err != nil {
		delete(settings.Effective, "defaultProvider")
		delete(settings.Effective, "defaultModel")
		settings.ModelSelection = &ModelSelection{CatalogError: err.Error()}
		return settings, nil
	}
	selected, invalid := resolveModelSelection(catalog, settings.Global, settings.PersonalGlobal)
	settings.Effective["defaultProvider"] = selected.Provider
	settings.Effective["defaultModel"] = selected.Model
	settings.ModelSelection = &ModelSelection{InvalidPersonal: invalid, Source: "first"}
	if current, ok := modelPair(settings.PersonalGlobal); ok && !invalid && current == selected {
		settings.ModelSelection.Source = "personal"
	} else if admin, ok := modelPair(settings.Global); ok && admin.Provider == selected.Provider && admin.Model == selected.Model {
		settings.ModelSelection.Source = "admin"
	}
	return settings, nil
}

func validateAccountModelPatch(patch map[string]any, catalog WorkspaceModelsResponse) error {
	provider, hasProvider := patch["defaultProvider"]
	model, hasModel := patch["defaultModel"]
	if hasProvider != hasModel {
		return errors.New("provider and model must be set together")
	}
	if hasProvider {
		if provider == nil && model == nil {
			return nil
		}
		p, pOK := provider.(string)
		m, mOK := model.(string)
		if !pOK || !mOK || strings.TrimSpace(p) == "" || strings.TrimSpace(m) == "" ||
			!catalogContains(catalog, p, m) {
			return errors.New("model is not in the configured catalog")
		}
	}
	return nil
}
