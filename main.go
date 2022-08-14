package main

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/ilyakaznacheev/cleanenv"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
)

// Config is an application configuration structure
type Config struct {
	Dirs []ConfigRule `yaml:"dirs"`
}

type ConfigRule struct {
	Path string `yaml:"path"`
	Rule string `yaml:"rule"`
}

type DirInfoStruct struct {
	size  int64
	files int
	dirs  int
}

func DirSize(path string) (DirInfoStruct, error) {
	var info = DirInfoStruct{}
	err := filepath.Walk(path, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			//err = fmt.Errorf("WalkError: %w", err)
			//color.HiYellow(err.Error())
			//return err
		} else {
			//fmt.Println(Path)
			if !fileInfo.IsDir() {
				info.files++
				info.size += fileInfo.Size()
			} else {
				info.dirs++
			}
		}
		return nil
	})
	return info, err
}

func HumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func GetFreeSpace() (int64, error) {
	wd, err := os.Getwd()
	var stat unix.Statfs_t

	err = unix.Statfs(wd, &stat)
	if err != nil {
		color.HiRed(err.Error())
		return 0, err
	}

	// Available blocks * size per block = available space in bytes
	return int64(stat.Bavail) * stat.Bsize, nil
}

func main() {
	var cfg Config
	err := cleanenv.ReadConfig("config.yaml", &cfg)
	if err != nil {
		color.HiRed(err.Error())
		return
	}

	// for each directory
	for _, rule := range cfg.Dirs {
		dirInfo, err := DirSize(rule.Path)
		if err != nil {
			color.HiRed(err.Error())
			//continue
		}
		fmt.Println("size of " + rule.Path + ": " + HumanSize(dirInfo.size) +
			" files: " + strconv.Itoa(dirInfo.files) +
			" dirs: " + strconv.Itoa(dirInfo.dirs))
	}

	// calculate free space
	var space, _ = GetFreeSpace()
	fmt.Println("Free space:", HumanSize(space))
}
