package utils

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Example:

    files, err = FilesFromRoot("./dir", func(dir string, name string, info os.FileInfo) bool {
		return info.Mode()&0111 != 0
	})
	if err != nil {
		fmt.Printf("FilesFromRoot: %v", err)
	}
	dirPaths = []string{}
	for dirPath := range files {
		dirPaths = append(dirPaths, dirPath)
	}
	sort.Strings(dirPaths)

	for _, dirPath := range dirPaths {
		fmt.Printf("%s\n", dirPath)
		for file := range files[dirPath] {
			fmt.Printf("  %s\n", file)
		}
	}

*/

// FilesFromRoot returns a map with path and array of files under it
func FilesFromRoot(root string, filterFn func(dir string, name string, info os.FileInfo) bool) (files map[string]map[string]string, err error) {
	files = make(map[string]map[string]string)

	symlinkedDirs, err := WalkSymlinks(root, "", files, filterFn)
	if err != nil {
		return nil, err
	}
	if len(symlinkedDirs) == 0 {
		return files, nil
	}

	walkedSymlinks := map[string]string{}

	// recurse list of symlinked directories
	// limit depth to stop symlink loops
	maxSymlinkDepth := 16
	for {
		maxSymlinkDepth--
		if maxSymlinkDepth == 0 {
			break
		}

		newSymlinkedDirs := map[string]string{}

		for origPath, target := range symlinkedDirs {
			symlinked, err := WalkSymlinks(target, origPath, files, filterFn)
			if err != nil {
				return nil, err
			}
			for k, v := range symlinked {
				newSymlinkedDirs[k] = v
			}
		}

		for k := range walkedSymlinks {
			delete(newSymlinkedDirs, k)
		}

		if len(newSymlinkedDirs) == 0 {
			break
		}

		symlinkedDirs = newSymlinkedDirs
	}

	return files, nil
}

func SymlinkInfo(path string, info os.FileInfo) (target string, isDir bool, err error) {
	// return empty path if not a symlink
	if info.Mode()&os.ModeSymlink == 0 {
		return "", false, nil
	}

	// Eval symlink path and get stat of a target path

	target, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, err
	}
	// is it file or dir?
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return "", false, err
	}

	return target, targetInfo.IsDir(), nil
}

// WalkSymlinks walks a directory, updates files map and returns symlinked directories
func WalkSymlinks(target string, linkName string, files map[string]map[string]string, filterFn func(dir string, name string, info os.FileInfo) bool) (symlinkedDirectories map[string]string, err error) {
	symlinkedDirectories = map[string]string{}

	err = filepath.Walk(target, func(foundPath string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("failure accessing a path '%s': %v\n", foundPath, err)
			return err
		}

		if info.IsDir() {
			return nil
		}

		resPath := foundPath
		if linkName != "" {
			// replace target with linkName in foundPath
			resPath = path.Join(linkName, strings.TrimPrefix(foundPath, target))
		}

		target, isDir, err := SymlinkInfo(foundPath, info)
		if err != nil {
			return err
		}
		if target != "" && isDir {
			// symlink to directory -> save it for future listing
			symlinkedDirectories[resPath] = target
			return nil
		}

		// Walk found a file or symlink to file, just store it.
		// FIXME symlink can have +x, but target file is not, so filterFn is not working properly
		fDir := path.Dir(resPath)
		fName := path.Base(resPath)
		if filterFn == nil || filterFn(fDir, fName, info) {
			if _, has := files[fDir]; !has {
				files[fDir] = map[string]string{}
			}
			files[fDir][fName] = ""
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk symlinks dir %s: %v", target, err)
	}

	return symlinkedDirectories, nil
}

// FindExecutableFilesInPath returns a list of executable and a list of non-executable files in path
func FindExecutableFilesInPath(dir string) (executables []string, nonExecutables []string, err error) {
	executables = make([]string, 0)

	nonExecutables = make([]string, 0)

	// Find only executable files
	files, err := FilesFromRoot(dir, func(dir string, name string, info os.FileInfo) bool {
		if info.Mode()&0111 != 0 {
			return true
		}
		nonExecutables = append(nonExecutables, path.Join(dir, name))
		return false
	})
	if err != nil {
		return
	}

	for dirPath, filePaths := range files {
		for file := range filePaths {
			executables = append(executables, path.Join(dirPath, file))
		}
	}

	sort.Strings(executables)

	return
}
