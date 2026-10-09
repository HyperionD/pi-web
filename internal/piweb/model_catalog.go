package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type configuredProvider struct {
	Models []struct {
		ID string `json:"id"`
	} `json:"models"`
}

type configuredFile struct {
	Providers map[string]configuredProvider `json:"providers"`
}

func modelConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find Pi model configuration: %w", err)
	}
	return filepath.Join(home, ".pi", "agent", "models.json"), nil
}

func configuredModels(ctx context.Context, root string) (WorkspaceModelsResponse, error) {
	path, err := modelConfigPath()
	if err != nil {
		return WorkspaceModelsResponse{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkspaceModelsResponse{}, fmt.Errorf("read Pi models.json: %w", err)
	}
	var config configuredFile
	if err := json.Unmarshal(data, &config); err != nil {
		return WorkspaceModelsResponse{}, errors.New("invalid Pi models.json")
	}
	allowed, err := configuredModelIDs(config)
	if err != nil {
		return WorkspaceModelsResponse{}, err
	}
	available, err := WorkspaceModels(ctx, root)
	if err != nil {
		return WorkspaceModelsResponse{}, errors.New("cannot list Pi models")
	}
	catalog := WorkspaceModelsResponse{Providers: []ModelProvider{}}
	for _, provider := range available.Providers {
		models := []ModelOption{}
		for _, model := range provider.Models {
			if _, ok := allowed[provider.ID+"\x00"+model.ID]; ok {
				models = append(models, model)
			}
		}
		if len(models) > 0 {
			catalog.Providers = append(catalog.Providers, ModelProvider{ID: provider.ID, Models: models})
		}
	}
	if len(catalog.Providers) == 0 {
		return WorkspaceModelsResponse{}, errors.New("no configured Pi models are available")
	}
	sort.Slice(catalog.Providers, func(i, j int) bool { return catalog.Providers[i].ID < catalog.Providers[j].ID })
	return catalog, nil
}

func configuredModelIDs(config configuredFile) (map[string]struct{}, error) {
	allowed := map[string]struct{}{}
	for provider, entry := range config.Providers {
		if strings.TrimSpace(provider) == "" {
			return nil, errors.New("Pi models.json has an empty provider ID")
		}
		for _, model := range entry.Models {
			if strings.TrimSpace(model.ID) == "" {
				return nil, errors.New("Pi models.json has an empty model ID")
			}
			key := provider + "\x00" + model.ID
			if _, exists := allowed[key]; exists {
				return nil, errors.New("Pi models.json has duplicate model IDs")
			}
			allowed[key] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("Pi models.json has no models")
	}
	return allowed, nil
}

func catalogContains(catalog WorkspaceModelsResponse, provider, model string) bool {
	for _, entry := range catalog.Providers {
		if entry.ID != provider {
			continue
		}
		for _, option := range entry.Models {
			if option.ID == model {
				return true
			}
		}
	}
	return false
}
