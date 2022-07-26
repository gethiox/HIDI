package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Entry struct {
	path string

	listed      bool
	dirs, files map[string]Entry
}

func NewEntry(path string) Entry {
	return Entry{
		path: path,
	}
}

func (e *Entry) list() error {
	fd, err := os.Open(e.path)
	if err != nil {
		return fmt.Errorf("cannot open \"%s\" file: %s", e.path, err)
	}
	defer fd.Close()

	info, err := fd.Stat()
	if !info.IsDir() {
		newPath, err := filepath.EvalSymlinks(e.path)
		if err != nil {
			return fmt.Errorf("cannot resolve symlink: %s", err)
		}

		fd, err = os.Open(newPath)
		if err != nil {
			return fmt.Errorf("cannot open \"%s\" file: %s", newPath, err)
		}
		defer fd.Close()
	}

	entries, err := fd.ReadDir(0)
	if err != nil {
		return fmt.Errorf("cannot read \"%s\" directory: %s", fd.Name(), err)
	}

	var dirs, files = make(map[string]Entry), make(map[string]Entry, 0)

	for _, entry := range entries {
		path := strings.Join([]string{e.path, entry.Name()}, string(os.PathSeparator))
		if entry.IsDir() {
			dirs[filepath.Base(path)] = NewEntry(path)
		} else {
			_, err := filepath.EvalSymlinks(path)
			if err != nil {
				files[filepath.Base(path)] = NewEntry(path)
			} else {
				dirs[filepath.Base(path)] = NewEntry(path)
			}
		}
	}
	e.dirs = dirs
	e.files = files
	e.listed = true
	return nil
}

func (e *Entry) Dirs() (map[string]Entry, error) {
	if !e.listed {
		err := e.list()
		if err != nil {
			return map[string]Entry{}, err
		}
	}
	return e.dirs, nil
}

func (e *Entry) Files() (map[string]Entry, error) {
	if !e.listed {
		err := e.list()
		if err != nil {
			return map[string]Entry{}, err
		}
	}
	return e.files, nil
}

func (e *Entry) Path() string {
	return e.path
}
