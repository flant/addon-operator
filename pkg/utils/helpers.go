package utils

import "os"

func DumpData(filePath string, data []byte) error {
	err := os.WriteFile(filePath, data, 0o644)
	if err != nil {
		return err
	}
	return nil
}

func CreateEmptyWritableFile(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return nil
	}

	_ = file.Close()
	return nil
}
