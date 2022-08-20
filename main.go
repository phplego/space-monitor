package main

import (
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jedib0t/go-pretty/v6/table"
	"golang.org/x/sys/unix"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v2"
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

	// paths and files
	gDataDir = GetAppDir() + "/data"
)

// Config is an application configuration structure
type Config struct {
	Dirs []Config_DirectorySettings `yaml:"dirs"`
}

type Config_DirectorySettings struct {
	Path string `yaml:"path"`
}

type DirInfoStruct struct {
	Path      string    `yaml:"path"`
	Size      int64     `yaml:"size"`
	Files     int       `yaml:"files"`
	Dirs      int       `yaml:"dirs"`
	ModTime   time.Time `yaml:"mtime"` // the time of the latest modified file in the directory
	StartTime time.Time `yaml:"stime"` // the time when the scan was started
}

func InitLogger() {
	var logFilename = GetAppDir() + "/space-monitor.log"
	file, err := os.OpenFile(logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}
	gLogger = *log.New(file, "", log.Ldate|log.Ltime|log.Lshortfile)
	gLogger.SetOutput(&lumberjack.Logger{
		Filename:   logFilename,
		MaxSize:    1, // megabytes after which new file is created
		MaxBackups: 1, // number of backups
		//MaxAge:     28, //days
	})
}

func InitConfig() {
	err := cleanenv.ReadConfig(GetAppDir()+"/config.yaml", &gCfg)
	if err != nil {
		color.HiRed(err.Error())
		return
	}
}

func GetAppDir() string {
	path, _ := os.Executable()
	return filepath.Dir(path)
}

func GetHash(text string) string {
	//h := xxh3.HashString128(text)
	//return fmt.Sprintf("%x%x", h.Hi, h.Lo)
	hash := sha1.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

func GetLastSnapshot(dir string, stepsBack int) DirInfoStruct {
	files, _ := filepath.Glob(gDataDir + fmt.Sprintf("/snapshot-*-%s.dat", GetHash(dir)))
	if files == nil {
		fmt.Printf("no snapshot files for %s\n", color.BlueString(dir))
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
	err := yaml.Unmarshal(bytes, &info)
	if err != nil {
		color.HiRed(err.Error())
		return DirInfoStruct{}
	}
	return info
}

func SaveDirInfo(path string, dirInfo DirInfoStruct) {
	pathHash := GetHash(path)
	os.Mkdir(gDataDir, 0777)
	snapshotFilePath := fmt.Sprintf(gDataDir+"/snapshot-%s-%s.dat", gStartTime.Format("2006-01-02 15:04:05"), pathHash)
	snapshotFile, err := os.OpenFile(snapshotFilePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		os.Exit(1)
	}
	bytes, _ := yaml.Marshal(dirInfo)
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

func ColorHeader(str string) string {
	my := color.New(color.FgHiBlue)
	my.Add(color.Bold)
	return my.Sprint(str)
}

func main() {
	gStartTime = time.Now()
	flag.Parse()

	InitLogger()
	InitConfig()

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(table.StyleRounded)
	tableWriter.SetOutputMirror(os.Stdout)
	tableWriter.AppendHeader(table.Row{"path", "Size", "Dirs", "Files", "last modified", "walk time"})

	var dirInfoPrev DirInfoStruct

	// for each directory
	for _, dir := range gCfg.Dirs {
		dirInfoPrev = GetLastSnapshot(dir.Path, 0)

		start := time.Now()
		dirInfo, err := ProcessDirectory(dir.Path)
		if err != nil {
			color.HiRed(err.Error())
			//continue
		}

		SaveDirInfo(dir.Path, dirInfo)

		deltaSize := ""
		if dirInfoPrev.Size != 0 && dirInfoPrev.Size != dirInfo.Size {
			if dirInfo.Size >= dirInfoPrev.Size {
				deltaSize = " (+" + HumanSize(dirInfo.Size-dirInfoPrev.Size) + ")"
			} else {
				deltaSize = " (" + HumanSize(dirInfo.Size-dirInfoPrev.Size) + ")"
			}
		}

		deltaDirs := ""
		if dirInfoPrev.Dirs != 0 && dirInfoPrev.Dirs != dirInfo.Dirs {
			deltaDirs = " (" + fmt.Sprintf("%+d", dirInfo.Dirs-dirInfoPrev.Dirs) + ")"
		}

		deltaFiles := ""
		if dirInfoPrev.Files != 0 && dirInfoPrev.Files != dirInfo.Files {
			deltaFiles = " (" + fmt.Sprintf("%+d", dirInfo.Files-dirInfoPrev.Files) + ")"
		}

		tableWriter.AppendRow([]interface{}{
			dir.Path,
			HumanSize(dirInfo.Size) + deltaSize,
			strconv.Itoa(dirInfo.Dirs) + deltaDirs,
			strconv.Itoa(dirInfo.Files) + deltaFiles,
			dirInfo.ModTime.Format(time.RFC822),
			time.Since(start).Round(time.Millisecond),
		})
	}

	tableWriter.AppendSeparator()
	tableWriter.AppendRow(table.Row{ColorHeader("start time"), gStartTime.Format(time.RFC822)})
	if !dirInfoPrev.StartTime.IsZero() {
		tableWriter.AppendRow(table.Row{ColorHeader("prev stime"), dirInfoPrev.StartTime.Format(time.RFC822)})
	}
	tableWriter.AppendSeparator()

	// calculate free space
	var space, _ = GetFreeSpace()

	tableWriter.AppendRow(table.Row{"FREE SPACE", color.HiGreenString(HumanSize(space)), "", "", "", time.Since(gStartTime).Round(time.Millisecond)})
	tableWriter.Render()

	if *gDaemonMode {
		fmt.Println("Running in daemon mode..")
		for {

		}
	}
}
