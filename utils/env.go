package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

func ExecPath() string {
	if path, err := os.Getwd(); err == nil {
		return path
	}
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)
	fmt.Println(exPath)
	return exPath
}
