package module_manager

import "os"

func CreateEmptyWritableFile(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil
	}

	_ = file.Close()
	return nil
}

