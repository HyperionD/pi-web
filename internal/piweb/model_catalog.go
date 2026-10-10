package piweb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func modelConfigPath() (string, error) {
	if agentDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); agentDir != "" {
		absolute, err := filepath.Abs(agentDir)
		if err != nil {
			return "", fmt.Errorf("find Pi agent directory: %w", err)
		}
		return filepath.Join(absolute, "models.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find Pi model configuration: %w", err)
	}
	return filepath.Join(home, ".pi", "agent", "models.json"), nil
}

func configuredModels(ctx context.Context, root string) (WorkspaceModelsResponse, error) {
	models, err := WorkspaceModels(ctx, root)
	if err != nil {
		return WorkspaceModelsResponse{}, errors.New("cannot list Pi models")
	}
	count := 0
	for _, provider := range models.Providers {
		count += len(provider.Models)
	}
	if count == 0 {
		return WorkspaceModelsResponse{}, errors.New("Pi has no available models")
	}
	return models, nil
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
