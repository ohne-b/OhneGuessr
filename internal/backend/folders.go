package backend

import (
	"errors"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
)

func (s *mapStore) createFolder(parent, name string) (string, error) {
	parent, err := normalizeRelative(parent)
	if err != nil {
		return "", errInvalidFolder
	}
	name, err = validateFolderName(name)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifestLocked()
	if err != nil {
		return "", err
	}
	if parent != "" {
		canonical, found := findFolder(manifest, parent)
		if !found {
			return "", errFolderNotFound
		}
		parent = canonical
	}
	if policy, managed := s.policyForFolder(parent); managed && !policy.EditableFolders {
		return "", errManagedFolder
	}
	folder := path.Join(parent, name)
	if s.isManagedRoot(folder) {
		return "", errManagedFolder
	}
	if hasFolder(manifest, folder) {
		return "", errFolderExists
	}
	filename, err := s.resolve(folder)
	if err != nil {
		return "", errInvalidFolder
	}
	if _, statErr := os.Stat(filename); statErr == nil {
		return "", errFolderExists
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err := os.Mkdir(filename, 0o755); err != nil {
		return "", err
	}
	manifest.Folders = append(manifest.Folders, folder)
	err = s.saveManifestLocked(manifest)
	if err != nil {
		_ = os.Remove(filename)
		return "", err
	}
	return folder, nil
}

func (s *mapStore) renameFolder(folder, name string) (string, error) {
	folder, err := normalizeRelative(folder)
	if err != nil || folder == "" {
		return "", errInvalidFolder
	}
	name, err = validateFolderName(name)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifestLocked()
	if err != nil {
		return "", err
	}
	canonical, found := findFolder(manifest, folder)
	if !found {
		return "", errFolderNotFound
	}
	folder = canonical
	if policy, managed := s.policyForFolder(folder); managed &&
		(strings.EqualFold(folder, policy.Root) || !policy.EditableFolders) {
		return "", errManagedFolder
	}
	parent := mapfile.Folder(folder)
	target := path.Join(parent, name)
	if s.isManagedRoot(target) {
		return "", errManagedFolder
	}
	if strings.EqualFold(folder, target) && folder == target {
		return folder, nil
	}
	if !strings.EqualFold(folder, target) && hasFolder(manifest, target) {
		return "", errFolderExists
	}
	oldPath, err := s.resolve(folder)
	if err != nil {
		return "", errInvalidFolder
	}
	newPath, err := s.resolve(target)
	if err != nil {
		return "", errInvalidFolder
	}
	if !strings.EqualFold(folder, target) {
		if _, statErr := os.Stat(newPath); statErr == nil {
			return "", errFolderExists
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return "", err
	}
	for index := range manifest.Maps {
		entry := &manifest.Maps[index]
		if !mapfile.UnderRoot(entry.File, folder) {
			continue
		}
		entry.File = path.Join(target, strings.TrimPrefix(entry.File, folder+"/"))
		if policy, managed := s.policyForSource(entry.Source); managed && policy.UpdateSource != nil {
			entry.Source = policy.UpdateSource(entry.Source, false, true)
		}
	}
	for index, current := range manifest.Folders {
		if !mapfile.UnderRoot(current, folder) {
			continue
		}
		if strings.EqualFold(current, folder) {
			manifest.Folders[index] = target
		} else {
			manifest.Folders[index] = path.Join(target, strings.TrimPrefix(current, folder+"/"))
		}
	}
	err = s.saveManifestLocked(manifest)
	if err != nil {
		_ = os.Rename(newPath, oldPath)
		return "", err
	}
	return target, nil
}

func (s *mapStore) deleteFolder(folder string, recursive bool) ([]string, error) {
	folder, err := normalizeRelative(folder)
	if err != nil || folder == "" {
		return nil, errInvalidFolder
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifestLocked()
	if err != nil {
		return nil, err
	}
	canonical, found := findFolder(manifest, folder)
	if !found {
		return nil, errFolderNotFound
	}
	folder = canonical
	if policy, managed := s.policyForFolder(folder); managed &&
		!strings.EqualFold(folder, policy.Root) && !policy.EditableFolders {
		return nil, errManagedFolder
	}
	deleted := make([]string, 0)
	kept := make([]mapEntry, 0, len(manifest.Maps))
	for _, entry := range manifest.Maps {
		if mapfile.UnderRoot(entry.File, folder) {
			deleted = append(deleted, entry.ID)
		} else {
			kept = append(kept, entry)
		}
	}
	hasChildren := false
	for _, child := range manifest.Folders {
		if !strings.EqualFold(child, folder) && mapfile.UnderRoot(child, folder) {
			hasChildren = true
			break
		}
	}
	if !recursive && (len(deleted) > 0 || hasChildren) {
		return nil, errFolderNotEmpty
	}
	filename, err := s.resolve(folder)
	if err != nil {
		return nil, errInvalidFolder
	}
	if !recursive {
		entries, readErr := os.ReadDir(filename)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				return nil, errFolderNotFound
			}
			return nil, readErr
		}
		if len(entries) > 0 {
			return nil, errFolderNotEmpty
		}
	}
	staged, existed, err := stageRemoval(filename)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, errFolderNotFound
	}
	manifest.Maps = kept
	folders := manifest.Folders[:0]
	for _, current := range manifest.Folders {
		if !mapfile.UnderRoot(current, folder) {
			folders = append(folders, current)
		}
	}
	manifest.Folders = folders
	err = s.saveManifestLocked(manifest)
	if err != nil {
		_ = os.Rename(staged, filename)
		return nil, err
	}
	_ = os.RemoveAll(staged)
	return deleted, nil
}

func hasFolder(manifest mapManifest, folder string) bool {
	_, found := findFolder(manifest, folder)
	return found
}

func findFolder(manifest mapManifest, folder string) (string, bool) {
	for _, candidate := range manifest.Folders {
		if strings.EqualFold(candidate, folder) {
			return candidate, true
		}
	}
	return "", false
}

func addFolderParents(values map[string]string, folder string) {
	for folder != "" && folder != "." {
		values[strings.ToLower(folder)] = folder
		folder = mapfile.Folder(folder)
	}
}

func folderValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sortFold(result)
	return result
}

func sortFold(values []string) {
	sort.Slice(values, func(i, j int) bool {
		left, right := strings.ToLower(values[i]), strings.ToLower(values[j])
		if left == right {
			return values[i] < values[j]
		}
		return left < right
	})
}
