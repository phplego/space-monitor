package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type MyFileInfo struct {
	Path    string
	ModTime time.Time
}

func main() {
	aPath := "/etc"

	lastFiles := []MyFileInfo{}

	err := filepath.Walk(aPath, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			fmt.Println(err)
			//return err // return error if you want to break walking
		} else {
			lastFiles = append(lastFiles, MyFileInfo{
				Path:    path,
				ModTime: fileInfo.ModTime(),
			})

		}
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}

	sort.Slice(lastFiles, func(i, j int) bool {
		return lastFiles[i].ModTime.Before(lastFiles[j].ModTime)
	})

	for _, info := range lastFiles {
		fmt.Println(info.ModTime, info.Path)
	}

	//fmt.Println(lastFiles)
}
