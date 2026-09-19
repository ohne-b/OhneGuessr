package backend

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ohne-b/OhneGuessr/internal/mapfile"
	"github.com/ohne-b/OhneGuessr/internal/pluginhost"
)

func stageRemoval(filename string) (string, bool, error) {
	if _, err := os.Stat(filename); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".ohneguessr-delete-*")
	if err != nil {
		return "", false, err
	}
	staged := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(staged)
		return "", false, err
	}
	if err := os.Remove(staged); err != nil {
		return "", false, err
	}
	if err := os.Rename(filename, staged); err != nil {
		return "", false, err
	}
	return staged, true, nil
}

func (s *mapStore) uniqueFileLocked(folder, name string, reserved map[string]bool) (string, error) {
	return s.uniquePathLocked(folder, slugify(name)+".json", reserved)
}

func (s *mapStore) uniquePathLocked(folder, filename string, reserved map[string]bool) (string, error) {
	folder, err := normalizeRelative(folder)
	if err != nil {
		return "", err
	}
	extension := path.Ext(filename)
	stem := strings.TrimSuffix(filename, extension)
	for index := 1; ; index++ {
		suffix := ""
		if index > 1 {
			suffix = fmt.Sprintf("-%d", index)
		}
		rel := path.Join(folder, stem+suffix+extension)
		filename, err := s.resolve(rel)
		if err != nil {
			return "", err
		}
		_, statErr := os.Stat(filename)
		if !reserved[strings.ToLower(rel)] && errors.Is(statErr, os.ErrNotExist) {
			return rel, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
	}
}

func (s *mapStore) resolve(rel string) (string, error) {
	clean, err := normalizeRelative(rel)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(s.dir, filepath.FromSlash(clean))
	relative, err := filepath.Rel(s.dir, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path leaves maps directory")
	}
	return joined, nil
}

func (s *mapStore) openPublic(rel string) (*os.File, os.FileInfo, error) {
	clean, err := normalizeRelative(rel)
	if err != nil || clean == "" || !strings.EqualFold(path.Ext(clean), ".json") {
		return nil, nil, os.ErrNotExist
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return nil, nil, os.ErrNotExist
		}
	}
	file, err := s.root.Open(filepath.FromSlash(clean))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = os.ErrNotExist
		}
		return nil, nil, err
	}
	return file, info, nil
}

func (s *mapStore) openByID(id string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifestLocked()
	if err != nil {
		return nil, err
	}
	for _, entry := range manifest.Maps {
		if entry.ID != id {
			continue
		}
		file, _, err := s.openPublic(entry.File)
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w for %q", errMapDataMissing, entry.Name)
		}
		return file, err
	}
	return nil, errMapNotFound
}

func normalizeRelative(value string) (string, error) {
	value = strings.ReplaceAll(value, "\\", "/")
	osValue := filepath.FromSlash(value)
	if strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') ||
		filepath.IsAbs(osValue) || filepath.VolumeName(osValue) != "" {
		return "", errors.New("invalid relative path")
	}
	value = strings.Trim(value, "/")
	if value == "" {
		return "", nil
	}
	parts := strings.Split(value, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return "", errors.New("invalid relative path")
		}
		clean = append(clean, part)
	}
	result := strings.Join(clean, "/")
	osPath := filepath.FromSlash(result)
	if filepath.IsAbs(osPath) || filepath.VolumeName(osPath) != "" {
		return "", errors.New("invalid relative path")
	}
	return result, nil
}

func validateFolderName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, ".") ||
		strings.ContainsAny(value, `<>:"/\|?*`) || strings.TrimRight(value, ". ") != value {
		return "", errInvalidFolder
	}
	for _, char := range value {
		if char < 32 {
			return "", errInvalidFolder
		}
	}
	if len([]rune(value)) > localNameMaxRunes {
		return "", errNameTooLong
	}
	base := strings.ToLower(strings.SplitN(value, ".", 2)[0])
	if mapfile.IsWindowsName(base) {
		return "", errInvalidFolder
	}
	return value, nil
}

func slugify(value string) string {
	var output []rune
	dash := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			output = append(output, char)
			dash = false
		} else if len(output) > 0 && !dash {
			output = append(output, '-')
			dash = true
		}
	}
	result := strings.Trim(string(output), "-")
	if result == "" {
		result = "map"
	}
	runes := []rune(result)
	if len(runes) > 100 {
		result = strings.TrimRight(string(runes[:100]), "-")
	}
	return result
}

func (s *mapStore) policyForSource(source map[string]any) (pluginhost.MapPolicy, bool) {
	policy, found := s.mapPolicies[strings.ToLower(mapfile.SourceType(source))]
	return policy, found
}

func (s *mapStore) policyForFolder(folder string) (pluginhost.MapPolicy, bool) {
	var match pluginhost.MapPolicy
	found := false
	for _, policy := range s.mapPolicies {
		if mapfile.UnderRoot(folder, policy.Root) && (!found || len(policy.Root) > len(match.Root)) {
			match, found = policy, true
		}
	}
	return match, found
}

func (s *mapStore) isManagedSource(source map[string]any) bool {
	managed, _ := source["managed"].(bool)
	_, pluginManaged := s.policyForSource(source)
	return managed || pluginManaged
}

func (s *mapStore) isManagedRoot(folder string) bool {
	policy, found := s.policyForFolder(folder)
	return found && strings.EqualFold(folder, policy.Root)
}
