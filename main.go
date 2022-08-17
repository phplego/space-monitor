package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/sys/unix"
	"gopkg.in/natefinch/lumberjack.v2"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var (
	startTime time.Time
	logger    log.Logger
	cfg       Config

	daemonMode = flag.Bool("daemon", false, "Run in background")
)

// Config is an application configuration structure
type Config struct {
	Dirs []Config_DirectorySettings `yaml:"dirs"`
}

type Config_DirectorySettings struct {
	Path string `yaml:"path"`
	Rule string `yaml:"rule"`
}

type DirInfoStruct struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Files    int       `json:"files"`
	Dirs     int       `json:"dirs"`
	ModTime  time.Time `json:"mtime"` // time of the latest modified file in the directory
	walkTime int
}

func GetHash(text string) string {
	//h := xxh3.HashString128(text)
	//return fmt.Sprintf("%x%x", h.Hi, h.Lo)
	hash := sha1.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

func GetLastSnapshot(dirHash string) DirInfoStruct {
	files, _ := filepath.Glob(fmt.Sprintf("./snapshot-*-%s.dat", dirHash))
	if files == nil {
		fmt.Println("no files")
		return DirInfoStruct{}
	}
	sort.Strings(files)
	last := files[len(files)-1]
	bytes, _ := os.ReadFile(last)
	info := DirInfoStruct{}
	json.Unmarshal(bytes, &info)
	return info
}

func SaveDirInfo(path string, dirInfo DirInfoStruct) {
	pathHash := GetHash(path)
	snapshotName := fmt.Sprintf("snapshot-%s-%s.dat", startTime.Format("2006-01-02 15:04:05"), pathHash)
	snapshotFile, err := os.OpenFile("./"+snapshotName, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}
	bytes, _ := json.Marshal(dirInfo)
	snapshotFile.WriteString(string(bytes))
}

func ProcessDirectory(path string) (DirInfoStruct, error) {

	var info = DirInfoStruct{
		Path: path,
	}
	err := filepath.Walk(path, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			logger.Println(err)
			//return err // return error if you want to break walking
		} else {
			modTime := fileInfo.ModTime()
			if modTime.After(info.ModTime) && modTime.Before(time.Now() /*skip Files from the future*/) {
				info.ModTime = fileInfo.ModTime()
			}
			if fileInfo.IsDir() {
				info.Dirs++
			} else {
				info.Files++
				info.Size += fileInfo.Size()
			}

			//fmt.Println( /*GetHash*/ (path), fileInfo.IsDir(), fileInfo.Size())
		}
		return nil
	})

	return info, err
}

func HumanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func GetFreeSpace() (int64, error) {
	wd, err := os.Getwd()
	var stat unix.Statfs_t

	err = unix.Statfs(wd, &stat)
	if err != nil {
		color.HiRed(err.Error())
		return 0, err
	}

	// Available blocks * Size per block = available space in bytes
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func InitLogger() {
	file, err := os.OpenFile("./space-monitor.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}
	logger = *log.New(file, "", log.Ldate|log.Ltime|log.Lshortfile)
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
	startTime = time.Now()
	flag.Parse()

	mainStart := time.Now()
	InitLogger()
	LoadConfig()

	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"path", "Size", "Dirs", "Files", "modified", "walk time"})

	// for each directory
	for _, dir := range cfg.Dirs {

		dirInfoPrev := GetLastSnapshot(GetHash(dir.Path))

		start := time.Now()
		dirInfo, err := ProcessDirectory(dir.Path)
		if err != nil {
			color.HiRed(err.Error())
			//continue
		}

		SaveDirInfo(dir.Path, dirInfo)

		deltaSize := ""
		if dirInfo.Size >= dirInfoPrev.Size {
			deltaSize = " (+" + HumanSize(dirInfo.Size-dirInfoPrev.Size) + ")"
		} else {
			deltaSize = " (" + HumanSize(dirInfo.Size-dirInfoPrev.Size) + ")"
		}

		t.AppendRow([]interface{}{
			dir.Path,
			HumanSize(dirInfo.Size) + deltaSize,
			dirInfo.Dirs,
			dirInfo.Files,
			dirInfo.ModTime.Format(time.RFC822),
			time.Since(start).Round(time.Millisecond),
		})
	}

	// calculate free space
	var space, _ = GetFreeSpace()

	t.AppendSeparator()
	t.AppendFooter(table.Row{"free space", HumanSize(space)})
	t.Render()

	fmt.Println("total time", time.Since(mainStart).Round(time.Millisecond))

	if *daemonMode {
		fmt.Println("Running in daemon mode..")
		for {

		}
	}
}
