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
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	Dirs         []Config_DirectorySettings `yaml:"dirs"`
	MaxSnapshots int                        `yaml:"max-snapshots"`
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

type SnapshotStruct struct {
	FreeSpace int64 `yaml:"free-space"`
}

func ColorHeader(str string, a ...interface{}) string {
	return color.New(color.FgBlue).Add(color.Bold).Sprintf(str, a...)
}

func ColorHeaderHi(str string, a ...interface{}) string {
	return color.New(color.FgHiBlue).Add(color.Bold).Sprintf(str, a...)
}

func ColorPale(str string, a ...interface{}) string {
	return color.New(color.FgHiBlack).Add(color.Bold).Sprintf(str, a...)
}

func LogErr(v ...any) {
	gLogger.Println(v...)
	fmt.Println(v...)
}

func InitLogger() {
	var logFilename = GetAppDir() + "/space-monitor.log"
	file, err := os.OpenFile(logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		LogErr("error opening file: %v", err)
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
	// default config values
	gCfg.MaxSnapshots = 20
	err := cleanenv.ReadConfig(GetAppDir()+"/config.yaml", &gCfg)
	if err != nil {
		color.HiRed(err.Error())
		return
	}
}

func InitDataDirs() {
	os.Mkdir(gDataDir, 0777)
	os.Mkdir(gDataDir+"/"+gStartTime.Format("2006-01-02 15:04:05"), 0777)
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

func SaveDirInfo(path string, dirInfo DirInfoStruct) {
	pathHash := GetHash(path)
	dirInfoFilePath := fmt.Sprintf(gDataDir+"/%s/dirinfo-%s.dat", gStartTime.Format("2006-01-02 15:04:05"), pathHash)
	dirInfoFile, err := os.OpenFile(dirInfoFilePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		LogErr("error opening file: %v", err)
		os.Exit(1)
	}
	bytes, _ := yaml.Marshal(dirInfo)
	dirInfoFile.WriteString(string(bytes))
}

func LoadPrevDirInfo(dir string, stepsBack int) DirInfoStruct {
	files, _ := filepath.Glob(gDataDir + fmt.Sprintf("/*/dirinfo-%s.dat", GetHash(dir)))
	if files == nil {
		fmt.Printf("no dirinfo files for %s\n", color.BlueString(dir))
		return DirInfoStruct{}
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		LogErr("Error: out of bounds dirinfo array. index=" + strconv.Itoa(index))
	}
	last := files[index]
	bytes, _ := os.ReadFile(last)
	info := DirInfoStruct{}
	err := yaml.Unmarshal(bytes, &info)
	if err != nil {
		LogErr(err.Error())
	}
	return info
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
		LogErr(err.Error())
		return 0, err
	}

	// Available blocks * Size per block = available space in bytes
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func SaveSnapshot(snapshot SnapshotStruct) {
	snapshotFilePath := fmt.Sprintf(gDataDir+"/%s/snapshot.dat", gStartTime.Format("2006-01-02 15:04:05"))
	snapshotFile, err := os.OpenFile(snapshotFilePath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		LogErr("error opening snapshot file: %v", err)
		os.Exit(1)
	}
	bytes, _ := yaml.Marshal(snapshot)
	snapshotFile.WriteString(string(bytes))
}

func LoadPrevSnapshot(stepsBack int) SnapshotStruct {
	files, _ := filepath.Glob(gDataDir + fmt.Sprintf("/*/snapshot.dat"))
	if files == nil {
		LogErr("no snapshot files")
		return SnapshotStruct{}
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		LogErr("Error: out of bounds snapshot array. index=" + strconv.Itoa(index))
	}
	last := files[index]
	bytes, _ := os.ReadFile(last)
	snap := SnapshotStruct{}
	err := yaml.Unmarshal(bytes, &snap)
	if err != nil {
		color.HiRed(err.Error())
	}
	return snap
}

func DeleteOldSnapshots() {
	files, _ := ioutil.ReadDir(gDataDir)
	var dirs []fs.FileInfo
	for _, file := range files {
		if file.IsDir() {
			dirs = append(dirs, file)
		}
	}
	if len(dirs) <= gCfg.MaxSnapshots {
		return // no need to delete
	}
	sort.Slice(dirs, func(i, j int) bool { // sort dirs (older first)
		return strings.Compare(dirs[i].Name(), dirs[j].Name()) < 0
	})

	for _, dir := range dirs[0 : len(dirs)-gCfg.MaxSnapshots] {
		err := os.RemoveAll(gDataDir + "/" + dir.Name())
		if err != nil {
			LogErr(err.Error())
			return
		}
	}
}

func main() {
	gStartTime = time.Now()
	flag.Parse()

	InitLogger()
	InitConfig()
	InitDataDirs()

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(table.StyleRounded)
	tableWriter.SetOutputMirror(os.Stdout)
	tableWriter.AppendHeader(table.Row{"path", "Size", "Dirs", "Files", "last modified", "walk time"})

	var prevDirInfo DirInfoStruct

	// for each directory
	for _, dir := range gCfg.Dirs {
		prevDirInfo = LoadPrevDirInfo(dir.Path, 0)

		start := time.Now()
		dirInfo, err := ProcessDirectory(dir.Path)
		if err != nil {
			LogErr(err.Error())
			//continue
		}

		SaveDirInfo(dir.Path, dirInfo)

		deltaSize := ""
		if prevDirInfo.Size != 0 && prevDirInfo.Size != dirInfo.Size {
			var sign = "+"
			if dirInfo.Size < prevDirInfo.Size {
				sign = ""
			}
			deltaSize = " (" + sign + HumanSize(dirInfo.Size-prevDirInfo.Size) + ")"
		}

		deltaDirs := ""
		if prevDirInfo.Dirs != 0 && prevDirInfo.Dirs != dirInfo.Dirs {
			deltaDirs = " (" + fmt.Sprintf("%+d", dirInfo.Dirs-prevDirInfo.Dirs) + ")"
		}

		deltaFiles := ""
		if prevDirInfo.Files != 0 && prevDirInfo.Files != dirInfo.Files {
			deltaFiles = " (" + fmt.Sprintf("%+d", dirInfo.Files-prevDirInfo.Files) + ")"
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
	if !prevDirInfo.StartTime.IsZero() {
		tableWriter.AppendRow(table.Row{ColorHeader("prev stime"), ColorPale(prevDirInfo.StartTime.Format(time.RFC822))})
	}
	tableWriter.AppendRow(table.Row{ColorHeaderHi("start time"), gStartTime.Format(time.RFC822)})
	tableWriter.AppendSeparator()

	prevSnapshot := LoadPrevSnapshot(0)

	// calculate free space
	var space, _ = GetFreeSpace()
	SaveSnapshot(SnapshotStruct{space})

	deltaFreeSpace := ""
	if prevSnapshot.FreeSpace != 0 && prevSnapshot.FreeSpace != space {
		var sign = "+"
		if space < prevSnapshot.FreeSpace {
			sign = ""
		}
		deltaFreeSpace = " (" + sign + HumanSize(space-prevSnapshot.FreeSpace) + ")"
	}

	tableWriter.AppendRow(table.Row{"FREE SPACE", color.HiGreenString(HumanSize(space)) + deltaFreeSpace, "", "", "", time.Since(gStartTime).Round(time.Millisecond)})
	tableWriter.Render()

	DeleteOldSnapshots()

	if *gDaemonMode {
		fmt.Println("Running in daemon mode..")
		for {

		}
	}
}
