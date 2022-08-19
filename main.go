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
	"strconv"
	"time"
)

var (
	gStartTime time.Time
	gLogger    log.Logger
	gCfg       Config

	// command line arguments
	gReplast    = flag.Bool("replast", false, "Repeat last output (no scan)")
	gDaemonMode = flag.Bool("daemon", false, "Run in background")
)

const dataDir = "./data"

// Config is an application configuration structure
type Config struct {
	Dirs []Config_DirectorySettings `yaml:"dirs"`
}

type Config_DirectorySettings struct {
	Path string `yaml:"path"`
}

type DirInfoStruct struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Files     int       `json:"files"`
	Dirs      int       `json:"dirs"`
	ModTime   time.Time `json:"mtime"` // the time of the latest modified file in the directory
	StartTime time.Time `json:"stime"` // the time when the scan was started
}

func GetHash(text string) string {
	//h := xxh3.HashString128(text)
	//return fmt.Sprintf("%x%x", h.Hi, h.Lo)
	hash := sha1.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

func GetLastSnapshot(dirHash string, stepsBack int) DirInfoStruct {
	files, _ := filepath.Glob(dataDir + fmt.Sprintf("/snapshot-*-%s.dat", dirHash))
	if files == nil {
		fmt.Println("no files")
		return DirInfoStruct{}
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		fmt.Println("Error: out of bounds snapshot array. index=" + strconv.Itoa(index))
	}
	last := files[index]
	bytes, _ := os.ReadFile(last)
	info := DirInfoStruct{}
	json.Unmarshal(bytes, &info)
	return info
}

func SaveDirInfo(path string, dirInfo DirInfoStruct) {
	pathHash := GetHash(path)
	os.Mkdir(dataDir, 0777)
	snapshotName := fmt.Sprintf(dataDir+"/snapshot-%s-%s.dat", gStartTime.Format("2006-01-02 15:04:05"), pathHash)
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
		Path:      path,
		StartTime: gStartTime,
	}
	err := filepath.Walk(path, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			gLogger.Println(err)
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
	var abs = func(v int64) int64 {
		if v < 0 {
			return -v
		}
		return v
	}

	const unit = 1024
	if abs(bytes) < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; abs(n) >= unit; n /= unit {
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
	gLogger = *log.New(file, "", log.Ldate|log.Ltime|log.Lshortfile)
	gLogger.SetOutput(&lumberjack.Logger{
		Filename:   "./space-monitor.log",
		MaxSize:    1, // megabytes after which new file is created
		MaxBackups: 1, // number of backups
		//MaxAge:     28, //days
	})
}

func LoadConfig() {
	err := cleanenv.ReadConfig("config.yaml", &gCfg)
	if err != nil {
		color.HiRed(err.Error())
		return
	}
}

func main() {
	gStartTime = time.Now()
	flag.Parse()

	mainStart := time.Now()
	InitLogger()
	LoadConfig()

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(table.StyleRounded)
	tableWriter.SetOutputMirror(os.Stdout)
	tableWriter.AppendHeader(table.Row{"path", "Size", "Dirs", "Files", "last modified", "walk time"})

	var dirInfoPrev DirInfoStruct

	// for each directory
	for _, dir := range gCfg.Dirs {

		dirInfoPrev = GetLastSnapshot(GetHash(dir.Path), 0)

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

		tableWriter.AppendRow([]interface{}{
			dir.Path,
			HumanSize(dirInfo.Size) + deltaSize,
			dirInfo.Dirs,
			dirInfo.Files,
			dirInfo.ModTime.Format(time.RFC822),
			time.Since(start).Round(time.Millisecond),
		})
	}

	tableWriter.AppendSeparator()
	tableWriter.AppendRow(table.Row{"start time", gStartTime.Format(time.RFC822)})
	tableWriter.AppendRow(table.Row{"prev stime", dirInfoPrev.StartTime.Format(time.RFC822)})
	tableWriter.AppendSeparator()

	// calculate free space
	var space, _ = GetFreeSpace()

	tableWriter.AppendRow(table.Row{"FREE SPACE", HumanSize(space), "", "", "", time.Since(mainStart).Round(time.Millisecond)})
	tableWriter.Render()

	if *gDaemonMode {
		fmt.Println("Running in daemon mode..")
		for {

		}
	}
}
