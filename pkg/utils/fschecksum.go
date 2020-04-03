package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
)

func CalculateStringsChecksum(stringArr ...string) string {
	hasher := md5.New()
	sort.Strings(stringArr)
	for _, value := range stringArr {
		_, _ = hasher.Write([]byte(value))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func CalculateChecksumOfFile(path string) (string, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return CalculateStringsChecksum(string(content)), nil
}

func CalculateChecksumOfDirectory(dir string) (string, error) {
	res := ""

	var checkErr error
	files, err := FilesFromRoot(dir, func(dir string, name string, info os.FileInfo) bool {
		fPath := path.Join(dir, name)
		checksum, err := CalculateChecksumOfFile(fPath)
		if err != nil {
			// return only bad files for logging
			checkErr = err
			return true
		}
		res = CalculateStringsChecksum(res, checksum)
		// good files are skipped
		return false
	})
	if err != nil {
		return "", err
	}
	if checkErr != nil {
		return "", fmt.Errorf("calculate checksum of %+v: %v", files, err)
	}

	return res, nil
}

func CalculateChecksumOfPaths(paths ...string) (string, error) {
	res := ""

	for _, aPath := range paths {
		fileInfo, err := os.Stat(aPath)
		if err != nil {
			return "", err
		}

		var checksum string
		if fileInfo.IsDir() {
			checksum, err = CalculateChecksumOfDirectory(aPath)
		} else {
			checksum, err = CalculateChecksumOfFile(aPath)
		}

		if err != nil {
			return "", err
		}
		res = CalculateStringsChecksum(res, checksum)
	}

	return res, nil
}
