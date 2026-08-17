package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type File struct {
	Name          string    `json:"name"`
	Size          int64     `json:"size"`
	EncryptedSize int64     `json:"encrypted_size"`
	ContentHash   string    `json:"content_hash,omitempty"`
	MessageID     int       `json:"message_id"`
	ChatID        int64     `json:"chat_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Entry struct {
	Name        string
	IsDir       bool
	Size        int64
	ContentHash string
	Updated     time.Time
}

type persisted struct {
	Files       map[string]File      `json:"files"`
	Directories map[string]time.Time `json:"directories"`
}

type Index struct {
	mu    sync.RWMutex
	path  string
	files map[string]File
	dirs  map[string]time.Time
}

func New(filePath string) *Index {
	return &Index{path: filePath, files: make(map[string]File), dirs: make(map[string]time.Time)}
}

func Open(filePath string) (*Index, error) {
	index := New(filePath)
	content, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return index, nil
	}
	var data persisted
	if err := json.Unmarshal(content, &data); err == nil && data.Files != nil {
		if err := validateState(data.Files, data.Directories); err != nil {
			return nil, err
		}
		index.files, index.dirs = data.Files, data.Directories
		if index.dirs == nil {
			index.dirs = make(map[string]time.Time)
		}
		return index, nil
	}
	// Migrate the original flat map format.
	var files map[string]File
	if err := json.Unmarshal(content, &files); err != nil {
		return nil, errors.New("invalid index file")
	}
	if err := validateState(files, nil); err != nil {
		return nil, err
	}
	index.files = files
	return index, nil
}

func NormalizePath(name string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(name))
	if cleaned == "/" {
		return "/", nil
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || strings.ContainsRune(cleaned, '\x00') {
		return "", errors.New("invalid path")
	}
	return cleaned, nil
}

func (i *Index) Snapshot() ([]byte, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return json.Marshal(persisted{Files: i.files, Directories: i.dirs})
}

func (i *Index) Restore(content []byte) error {
	var data persisted
	if err := json.Unmarshal(content, &data); err != nil || data.Files == nil {
		return errors.New("invalid index backup")
	}
	if err := validateState(data.Files, data.Directories); err != nil {
		return err
	}
	if data.Directories == nil {
		data.Directories = make(map[string]time.Time)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.files, i.dirs = data.Files, data.Directories
	return i.saveLocked()
}

func (i *Index) Get(name string) (File, bool) {
	name, err := NormalizePath(name)
	if err != nil || name == "/" {
		return File{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	file, ok := i.files[name]
	return file, ok
}

func (i *Index) List() []File {
	i.mu.RLock()
	defer i.mu.RUnlock()
	files := make([]File, 0, len(i.files))
	for _, file := range i.files {
		files = append(files, file)
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Name < files[b].Name })
	return files
}

func (i *Index) DirectoryPaths() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	directories := make(map[string]struct{}, len(i.dirs))
	for directory := range i.dirs {
		directories[directory] = struct{}{}
	}
	for fileName := range i.files {
		for parent := path.Dir(fileName); parent != "." && parent != "/"; parent = path.Dir(parent) {
			directories[parent] = struct{}{}
		}
	}
	result := make([]string, 0, len(directories))
	for directory := range directories {
		result = append(result, directory)
	}
	sort.Strings(result)
	return result
}

func (i *Index) IsDir(directory string) bool {
	directory, err := NormalizePath(directory)
	if err != nil || directory == "/" {
		return err == nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if _, ok := i.dirs[directory]; ok {
		return true
	}
	prefix := directory + "/"
	for name := range i.files {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for name := range i.dirs {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (i *Index) Entries(directory string) []Entry {
	directory, err := NormalizePath(directory)
	if err != nil {
		return nil
	}
	if directory == "/" {
		directory = ""
	}
	prefix := directory
	if prefix != "" {
		prefix += "/"
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	entries := make(map[string]Entry)
	for name, file := range i.files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(name, prefix)
		parts := strings.SplitN(remainder, "/", 2)
		if len(parts) == 1 {
			entries[parts[0]] = Entry{Name: parts[0], Size: file.Size, ContentHash: file.ContentHash, Updated: file.UpdatedAt}
			continue
		}
		entries[parts[0]] = Entry{Name: parts[0], IsDir: true, Updated: file.UpdatedAt}
	}
	for name, updated := range i.dirs {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(name, prefix)
		parts := strings.SplitN(remainder, "/", 2)
		if len(parts) == 1 {
			entries[parts[0]] = Entry{Name: parts[0], IsDir: true, Updated: updated}
		}
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(a, b int) bool {
		if result[a].IsDir != result[b].IsDir {
			return result[a].IsDir
		}
		return result[a].Name < result[b].Name
	})
	return result
}

func (i *Index) Put(file File) error {
	name, err := NormalizePath(file.Name)
	if err != nil || name == "/" {
		return errors.New("invalid file path")
	}
	file.Name = name
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, exists := i.dirs[name]; exists {
		return errors.New("directory exists")
	}
	if hasFileAncestor(i.files, name) {
		return errors.New("parent path is a file")
	}
	prefix := name + "/"
	for child := range i.files {
		if strings.HasPrefix(child, prefix) {
			return errors.New("directory exists")
		}
	}
	for child := range i.dirs {
		if strings.HasPrefix(child, prefix) {
			return errors.New("directory exists")
		}
	}
	i.files[name] = file
	return i.saveLocked()
}
func (i *Index) Delete(name string) error {
	name, err := NormalizePath(name)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.files, name)
	return i.saveLocked()
}

func (i *Index) Mkdir(directory string) error {
	directory, err := NormalizePath(directory)
	if err != nil || directory == "/" {
		return errors.New("invalid directory")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, exists := i.files[directory]; exists {
		return errors.New("file exists")
	}
	if _, exists := i.dirs[directory]; exists {
		return errors.New("directory exists")
	}
	if hasFileAncestor(i.files, directory) {
		return errors.New("parent path is a file")
	}
	i.dirs[directory] = time.Now().UTC()
	return i.saveLocked()
}

func (i *Index) RemoveDir(directory string) error {
	directory, err := NormalizePath(directory)
	if err != nil || directory == "/" {
		return errors.New("invalid directory")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	prefix := directory + "/"
	for name := range i.files {
		if strings.HasPrefix(name, prefix) {
			return errors.New("directory not empty")
		}
	}
	for name := range i.dirs {
		if strings.HasPrefix(name, prefix) {
			return errors.New("directory not empty")
		}
	}
	if _, ok := i.dirs[directory]; !ok {
		return errors.New("directory not found")
	}
	delete(i.dirs, directory)
	return i.saveLocked()
}

func (i *Index) RemoveDirRecursive(directory string) error {
	directory, err := NormalizePath(directory)
	if err != nil || directory == "/" {
		return errors.New("invalid directory")
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	prefix := directory + "/"
	found := false
	for name := range i.files {
		if name == directory || strings.HasPrefix(name, prefix) {
			delete(i.files, name)
			found = true
		}
	}
	for name := range i.dirs {
		if name == directory || strings.HasPrefix(name, prefix) {
			delete(i.dirs, name)
			found = true
		}
	}
	if !found {
		return errors.New("directory not found")
	}
	return i.saveLocked()
}

func (i *Index) Rename(oldName, newName string) error {
	oldName, err := NormalizePath(oldName)
	if err != nil || oldName == "/" {
		return errors.New("invalid source path")
	}
	newName, err = NormalizePath(newName)
	if err != nil || newName == "/" {
		return errors.New("invalid target path")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if oldName == newName {
		return nil
	}
	if hasFileAncestor(i.files, newName) {
		return errors.New("parent path is a file")
	}
	if file, ok := i.files[oldName]; ok {
		if _, exists := i.files[newName]; exists {
			return errors.New("target exists")
		}
		if _, exists := i.dirs[newName]; exists || hasDescendant(i.files, newName) || hasDescendant(i.dirs, newName) {
			return errors.New("target exists")
		}
		delete(i.files, oldName)
		file.Name = newName
		i.files[newName] = file
		return i.saveLocked()
	}
	if _, ok := i.dirs[oldName]; !ok {
		prefix := oldName + "/"
		found := false
		for name := range i.files {
			if strings.HasPrefix(name, prefix) {
				found = true
				break
			}
		}
		if !found {
			for name := range i.dirs {
				if strings.HasPrefix(name, prefix) {
					found = true
					break
				}
			}
		}
		if !found {
			return errors.New("source not found")
		}
	}
	prefix := oldName + "/"
	if strings.HasPrefix(newName, prefix) {
		return errors.New("cannot move a directory inside itself")
	}
	if _, exists := i.files[newName]; exists || hasDescendant(i.files, newName) {
		return errors.New("target exists")
	}
	if _, exists := i.dirs[newName]; exists || hasDescendant(i.dirs, newName) {
		return errors.New("target exists")
	}
	for name := range i.files {
		if name == oldName || strings.HasPrefix(name, prefix) {
			target := newName + strings.TrimPrefix(name, oldName)
			if _, exists := i.files[target]; exists {
				return errors.New("target exists")
			}
		}
	}
	for name := range i.dirs {
		if name == oldName || strings.HasPrefix(name, prefix) {
			target := newName + strings.TrimPrefix(name, oldName)
			if _, exists := i.dirs[target]; exists {
				return errors.New("target exists")
			}
		}
	}
	movedFiles := make(map[string]File)
	for name, file := range i.files {
		if name == oldName || strings.HasPrefix(name, prefix) {
			delete(i.files, name)
			file.Name = newName + strings.TrimPrefix(name, oldName)
			movedFiles[file.Name] = file
		}
	}
	for name, file := range movedFiles {
		i.files[name] = file
	}
	movedDirs := make(map[string]time.Time)
	for name, updated := range i.dirs {
		if name == oldName || strings.HasPrefix(name, prefix) {
			delete(i.dirs, name)
			movedDirs[newName+strings.TrimPrefix(name, oldName)] = updated
		}
	}
	for name, updated := range movedDirs {
		i.dirs[name] = updated
	}
	return i.saveLocked()
}

func hasFileAncestor(files map[string]File, name string) bool {
	for parent := path.Dir(name); parent != "." && parent != "/"; parent = path.Dir(parent) {
		if _, exists := files[parent]; exists {
			return true
		}
	}
	return false
}

func hasDescendant[T any](entries map[string]T, name string) bool {
	prefix := name + "/"
	for entryName := range entries {
		if strings.HasPrefix(entryName, prefix) {
			return true
		}
	}
	return false
}

func validateState(files map[string]File, dirs map[string]time.Time) error {
	for name := range files {
		normalized, err := NormalizePath(name)
		if err != nil || normalized != name || name == "/" {
			return errors.New("invalid file path in index")
		}
		if hasFileAncestor(files, name) {
			return errors.New("file path conflicts with its parent")
		}
		if _, exists := dirs[name]; exists {
			return errors.New("file path conflicts with directory")
		}
	}
	for name := range dirs {
		normalized, err := NormalizePath(name)
		if err != nil || normalized != name || name == "/" {
			return errors.New("invalid directory path in index")
		}
		if hasFileAncestor(files, name) {
			return errors.New("directory path conflicts with its parent")
		}
		if _, exists := files[name]; exists {
			return errors.New("directory path conflicts with file")
		}
	}
	return nil
}

func (i *Index) saveLocked() error {
	if i.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(persisted{Files: i.files, Directories: i.dirs}, "", "  ")
	if err != nil {
		return err
	}
	temporary := i.path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, i.path)
}
