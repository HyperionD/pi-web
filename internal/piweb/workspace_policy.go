package piweb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrWorkspaceForbidden = errors.New("workspace path is outside allowed roots")
var ErrWorkspacePolicyInvalid = errors.New("workspace policy is invalid")

type WorkspacePolicy struct {
	allowedRoots []string
	restricted   bool
	invalid      bool
}

func NewWorkspacePolicy(config map[string]any) (WorkspacePolicy, error) {
	policy := WorkspacePolicy{}
	values, configured, err := workspaceAllowedRoots(config)
	if err != nil {
		policy.invalid = true
		return policy, fmt.Errorf("%w: %v", ErrWorkspacePolicyInvalid, err)
	}
	if !configured || len(values) == 0 {
		return policy, nil
	}

	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		root, ok := value.(string)
		if !ok || strings.TrimSpace(root) == "" {
			policy.invalid = true
			return policy, fmt.Errorf("%w: allowedRoots[%d] must be a non-empty string", ErrWorkspacePolicyInvalid, index)
		}
		realRoot, err := resolveExistingDirectory(root)
		if err != nil {
			policy.invalid = true
			return policy, fmt.Errorf("%w: allowedRoots[%d]: %v", ErrWorkspacePolicyInvalid, index, err)
		}
		if _, exists := seen[realRoot]; exists {
			continue
		}
		seen[realRoot] = struct{}{}
		policy.allowedRoots = append(policy.allowedRoots, realRoot)
	}
	policy.restricted = len(policy.allowedRoots) > 0
	return policy, nil
}

func LoadWorkspacePolicy() (WorkspacePolicy, error) {
	config, err := LoadAppConfig()
	if err != nil {
		return WorkspacePolicy{invalid: true}, fmt.Errorf("%w: load app config: %v", ErrWorkspacePolicyInvalid, err)
	}
	return NewWorkspacePolicy(config.Config)
}

func workspaceAllowedRoots(config map[string]any) ([]any, bool, error) {
	workspaceValue, exists := config["workspace"]
	if !exists || workspaceValue == nil {
		return nil, false, nil
	}
	workspace, ok := workspaceValue.(map[string]any)
	if !ok {
		return nil, false, errors.New("workspace must be an object")
	}
	value, exists := workspace["allowedRoots"]
	if !exists || value == nil {
		return nil, false, nil
	}
	switch roots := value.(type) {
	case []any:
		return roots, true, nil
	case []string:
		values := make([]any, len(roots))
		for index, root := range roots {
			values[index] = root
		}
		return values, true, nil
	default:
		return nil, false, errors.New("workspace.allowedRoots must be an array of strings")
	}
}

func (p WorkspacePolicy) CheckExistingDirectory(path string) error {
	if p.invalid {
		return ErrWorkspacePolicyInvalid
	}
	realPath, err := resolveExistingDirectory(path)
	if err != nil {
		return err
	}
	return p.checkResolvedPath(realPath)
}

func (p WorkspacePolicy) CheckFolderPath(path string) error {
	return p.CheckExistingDirectory(path)
}

func (p WorkspacePolicy) AllowedRootListing(path string) (FolderListing, error) {
	if !p.restricted {
		return ListFolders(path)
	}
	clean, err := resolveExistingDirectory(path)
	if err != nil {
		return FolderListing{}, err
	}
	folders := make([]FolderEntry, 0, len(p.allowedRoots))
	for _, root := range p.allowedRoots {
		folders = append(folders, FolderEntry{
			Name:        filepath.Base(root),
			Path:        root,
			DisplayPath: DisplayPath(root),
		})
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})
	parent := filepath.Dir(clean)
	if parent == clean {
		parent = ""
	}
	return FolderListing{
		Path:              clean,
		DisplayPath:       DisplayPath(clean),
		RootListing:       true,
		Parent:            parent,
		ParentDisplayPath: DisplayPath(parent),
		Folders:           folders,
	}, nil
}

func (p WorkspacePolicy) FilterFolderListing(listing FolderListing) (FolderListing, error) {
	if !p.restricted {
		return listing, nil
	}
	filtered := listing
	filtered.Folders = filtered.Folders[:0]
	for _, folder := range listing.Folders {
		err := p.CheckExistingDirectory(folder.Path)
		if errors.Is(err, ErrWorkspaceForbidden) {
			continue
		}
		if err != nil {
			return FolderListing{}, err
		}
		filtered.Folders = append(filtered.Folders, folder)
	}
	return filtered, nil
}

func (p WorkspacePolicy) ValidateCloneDestination(parentPath, name string) (string, error) {
	if p.invalid {
		return "", ErrWorkspacePolicyInvalid
	}
	parent, err := resolveExistingDirectory(parentPath)
	if err != nil {
		return "", err
	}
	if err := p.checkResolvedPath(parent); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) || strings.ContainsRune(name, 0) {
		return "", errors.New("clone destination name is invalid")
	}

	destination := filepath.Join(parent, name)
	if _, err := os.Lstat(destination); err == nil {
		if err := p.CheckExistingDirectory(destination); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return destination, nil
}

func (p WorkspacePolicy) checkResolvedPath(path string) error {
	if !p.restricted {
		return nil
	}
	for _, root := range p.allowedRoots {
		if pathWithinRoot(root, path) {
			return nil
		}
	}
	return ErrWorkspaceForbidden
}

func resolveExistingDirectory(path string) (string, error) {
	expanded, err := expandAppConfigPath(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", errors.New("workspace path is empty")
	}
	absolute, err := filepath.Abs(filepath.Clean(expanded))
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	realPath = filepath.Clean(realPath)
	info, err := os.Stat(realPath)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path must be an existing directory")
	}
	return realPath, nil
}

func pathWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
