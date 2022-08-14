package main

import (
	"fmt"
	"github.com/fatih/color1"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/sys/unix"
	"gopkg.in/natefinch/lumberjack.v2"
	"log"
	"os"
	"path/filepath"
)

var (
	logger log.Logger
	cfg    Config
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
			logger.Println(err)
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

func InitLogger() {
	e, err := os.OpenFile("./space-monitor.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}
	logger = *log.New(e, "", log.Ldate|log.Ltime|log.Lshortfile)
	logger.SetOutput(&lumberjack.Logger{
		Filename:   "./space-monitor.log",
		MaxSize:    1, // megabytes after which new file is created
		MaxBackups: 1, // number of backups
		//MaxAge:     28, //days
	})
}

func LoadConfig() {
	err := cleanenv.ReadConfig("config.yaml", &cfg)
	if err != nil {
		color.HiRed(err.Error())
		return
	}
}

func main() {
	InitLogger()
	LoadConfig()

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"path", "size", "dirs", "files"})

	// for each directory
	for _, rule := range cfg.Dirs {
		dirInfo, err := DirSize(rule.Path)
		if err != nil {
			color.HiRed(err.Error())
			//continue
		}
		t.AppendRow([]interface{}{rule.Path, HumanSize(dirInfo.size), dirInfo.dirs, dirInfo.files})
	}

	// calculate free space
	var space, _ = GetFreeSpace()

	t.AppendSeparator()
	t.AppendFooter(table.Row{"free space", HumanSize(space)})
	t.Render()
}
