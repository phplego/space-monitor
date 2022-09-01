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

// Max returns the larger of x or y.
func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// Min returns the smaller of x or y.
func Min(x, y int) int {
	if x > y {
		return y
	}
	return x
}

func GetLastFiles(aPath string, count int) ([]MyFileInfo, int) {
	var result []MyFileInfo
	var total = 0
	err := filepath.Walk(aPath, func(path string, fileInfo os.FileInfo, err error) error {
		total++
		if err != nil {
			fmt.Println(err)
		} else {

			if len(result) == count {
				// skip older files
				if fileInfo.ModTime().Before(result[0].ModTime) {
					return nil
				}
			}
			result = append(result, MyFileInfo{
				Path:    path,
				ModTime: fileInfo.ModTime(),
			})

			// sort by mod time (older first)
			sort.Slice(result, func(i, j int) bool {
				return result[i].ModTime.Before(result[j].ModTime)
			})
			start := Max(0, len(result)-count)
			result = result[start:]
		}
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}

	return result, total
}

func main() {
	aPath := "/home"
	lastFiles, total := GetLastFiles(aPath, 50)
	for i, info := range lastFiles {
		fmt.Printf("%02d| %v %s\n", i, info.ModTime, info.Path)
	}
	fmt.Printf("TOTAL: %02d\n", total)
}
