package main

import (
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jedib0t/go-pretty/v6/table"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v2"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	gStartTime time.Time = time.Now() // application start time
	gLogger    log.Logger
	gCfg       Config

	// command line arguments
	gRepLast    = flag.Bool("replast", false, "Repeat last results")
	gNoSave     = flag.Bool("nosave", false, "Don't save state")
	gDaemonMode = flag.Bool("daemon", false, "Run in background")

	// paths and files
	gDataDir = GetAppDir() + "/data"
)

// Config is an application configuration structure
type Config struct {
	Dirs         []Config_DirectorySettings `yaml:"dirs"`
	MaxSnapshots int                        `yaml:"max-snapshots"`
	DetailedMode bool                       `yaml:"detailed-mode"`
}

type Config_DirectorySettings struct {
	Path string `yaml:"path"`
}

type DirInfoStruct struct {
	Path         string    `yaml:"path"`
	Size         int64     `yaml:"size"`
	Files        int       `yaml:"files"`
	Dirs         int       `yaml:"dirs"`
	ModTime      time.Time `yaml:"mtime"` // the time of the latest modified file in the directory
	StartTime    time.Time `yaml:"stime"` // the time when the scan was started
	walkDuration time.Duration
	fileMap      map[string]GobFileInfo // for detailed mode
}

type GobFileInfo struct {
	Size int64
}

type SnapshotStruct struct {
	FreeSpace int64     `yaml:"free-space"`
	StartTime time.Time `yaml:"start-time"`
	infoList  []DirInfoStruct
}

func LogErr(v ...any) {
	gLogger.Println(v...)
	color.New(color.FgHiRed, color.Italic).Println(v...)
}

func GetSnapshotDirectory() string {
	return gDataDir + "/" + gStartTime.Format("2006-01-02 15:04:05")
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
	gCfg.DetailedMode = false

	// load file
	err := cleanenv.ReadConfig(GetAppDir()+"/config.yaml", &gCfg)
	if err != nil {
		LogErr(err)
	}
}

func InitDataDirs() {
	err := os.Mkdir(gDataDir, 0777)
	if err != nil && !errors.Is(err, os.ErrExist) {
		LogErr(err)
	}
	if !*gNoSave && !*gRepLast {
		err = os.Mkdir(GetSnapshotDirectory(), 0777)
		if err != nil && !errors.Is(err, os.ErrExist) {
			LogErr(err)
		}
	}
}

func InitStdoutSaver() {
	if *gNoSave || *gRepLast { // don't save report.txt when nosave mode or replast option
		return
	}
	reportFile, _ := os.OpenFile(GetSnapshotDirectory()+"/report.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	origStdout := os.Stdout
	multiWriter := io.MultiWriter(reportFile, origStdout)
	pipeReader, pipeWriter, _ := os.Pipe()
	os.Stdout = pipeWriter
	color.Output = os.Stdout
	go func() {
		for {
			_, err := io.Copy(multiWriter, pipeReader) // stucks forever
			if err != nil {
				origStdout.WriteString("io.Copy error: " + err.Error())
			}
		}
	}()
}

func SaveDirInfo(path string, dirInfo DirInfoStruct) {
	pathHash := GetHash(path)
	dirInfoFilePath := fmt.Sprintf(GetSnapshotDirectory()+"/dirinfo-%s.dat", pathHash)
	dirInfoFile, err := os.OpenFile(dirInfoFilePath, os.O_WRONLY|os.O_CREATE, 0666)
	// noinspection GoUnhandledErrorResult
	defer dirInfoFile.Close()
	if err != nil {
		LogErr(err)
		os.Exit(1)
	}

	if gCfg.DetailedMode {
		encodeFile, err := os.Create(strings.Replace(dirInfoFilePath, ".dat", ".gob", 1))
		// noinspection GoUnhandledErrorResult
		defer encodeFile.Close()
		if err != nil {
			LogErr(err)
			return
		}
		encoder := gob.NewEncoder(encodeFile)
		if err := encoder.Encode(dirInfo.fileMap); err != nil {
			LogErr(err)
		}
	}

	bytes, _ := yaml.Marshal(dirInfo)
	_, err = dirInfoFile.WriteString(string(bytes))
	if err != nil {
		LogErr(err)
	}
}

func LoadPrevDirInfo(dir string, stepsBack int) (DirInfoStruct, error) {
	files, _ := filepath.Glob(gDataDir + fmt.Sprintf("/*/dirinfo-%s.dat", GetHash(dir)))
	if files == nil {
		fmt.Println("no dirinfo files for", color.BlueString(dir), "is it first run?")
		return DirInfoStruct{}, errors.New("no prev dirinfo files")
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		return DirInfoStruct{}, errors.New("Error: out of bounds dirinfo array. index=" + strconv.Itoa(index))
	}
	last := files[index]
	bytes, _ := os.ReadFile(last)
	info := DirInfoStruct{}
	err := yaml.Unmarshal(bytes, &info)
	if err != nil {
		LogErr(err)
	}

	if gCfg.DetailedMode {
		decodeFile, err := os.Open(strings.Replace(last, ".dat", ".gob", 1))
		if err != nil {
			return info, err
		}
		// noinspection GoUnhandledErrorResult
		defer decodeFile.Close()
		decoder := gob.NewDecoder(decodeFile)
		err = decoder.Decode(&info.fileMap)
		if err != nil {
			return info, err
		}
	}

	return info, nil
}

func ProcessDirectory(dir string) (DirInfoStruct, error) {
	dir = AbsPath(dir)
	var info = DirInfoStruct{
		Path:      dir,
		StartTime: gStartTime,
		fileMap:   map[string]GobFileInfo{},
	}
	err := filepath.Walk(dir, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			gLogger.Println(err)
			//return err // return error if you want to break walking
		} else {
			if gCfg.DetailedMode {
				var size int64 = 0
				if !fileInfo.IsDir() {
					size = fileInfo.Size()
				}
				info.fileMap[path] = GobFileInfo{Size: size}
			}

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

func SaveSnapshot(snapshot SnapshotStruct) {
	snapshotFile, err := os.OpenFile(GetSnapshotDirectory()+"/snapshot.dat", os.O_WRONLY|os.O_CREATE, 0666)
	// noinspection GoUnhandledErrorResult
	defer snapshotFile.Close()
	if err != nil {
		LogErr("error opening snapshot file: %v", err)
		os.Exit(1)
	}
	bytes, _ := yaml.Marshal(snapshot)
	_, err = snapshotFile.WriteString(string(bytes))
	if err != nil {
		LogErr(err)
	}
}

func LoadPrevSnapshot(stepsBack int) SnapshotStruct {
	files, err := filepath.Glob(gDataDir + fmt.Sprintf("/*/snapshot.dat"))
	if err != nil {
		LogErr(err)
		os.Exit(1)
	}
	if files == nil {
		fmt.Println("no snapshot files; is it first run?")
		return SnapshotStruct{}
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		LogErr("Error: out of bounds snapshot array. len(files):", len(files), "stepsBack:", stepsBack)
		return SnapshotStruct{}
	}
	last := files[index]
	bytes, _ := os.ReadFile(last)
	snap := SnapshotStruct{}
	err = yaml.Unmarshal(bytes, &snap)
	if err != nil {
		LogErr(err)
	}
	return snap
}

func DeleteOldSnapshots() {
	files, _ := os.ReadDir(gDataDir)
	var dirs []fs.DirEntry
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
			LogErr(err)
			return
		}
	}
}

func PrintDiff(prevDirInfo, currDirInfo DirInfoStruct) {
	var prevMap = prevDirInfo.fileMap
	var currMap = currDirInfo.fileMap
	if len(prevMap) == 0 {
		return // skip empty map (eg. when first run)
	}
	colorModifiedDirPart := color.New(color.BgBlue, color.FgWhite)
	colorModified := color.New(color.FgHiBlue)
	colorAdded := color.New(color.FgHiGreen)
	colorAddedDirPart := color.New(color.BgGreen, color.FgWhite)
	colorDeleted := color.New(color.FgHiRed)
	colorDeletedDirPart := color.New(color.BgRed, color.FgWhite)
	colorDelta := color.New(color.FgHiMagenta)
	var addList []string
	var modList []string
	var delList []string

	for key := range currMap {
		if _, ok := prevMap[key]; !ok {
			addList = append(addList, key)
		} else {
			if currMap[key].Size != prevMap[key].Size {
				modList = append(modList, key)
			}
		}
	}
	for key := range prevMap {
		if _, ok := currMap[key]; !ok {
			delList = append(delList, key)
		}
	}

	sort.Strings(addList)
	sort.Strings(modList)
	sort.Strings(delList)
	for _, key := range addList {
		relPath := strings.Replace(key, AbsPath(currDirInfo.Path), "", 1)
		val := currMap[key]
		colorAdded.Print("+ ")
		colorAdded.Printf("%-10s", HumanSize(val.Size))
		colorAddedDirPart.Print(AbsPath(currDirInfo.Path))
		colorAdded.Print(relPath)
		fmt.Println()
	}
	for _, key := range modList {
		relPath := strings.Replace(key, AbsPath(currDirInfo.Path), "", 1)
		val := currMap[key]
		colorModified.Print("≈ ")
		colorModified.Printf("%-10s", HumanSize(val.Size))
		colorModifiedDirPart.Print(AbsPath(currDirInfo.Path))
		colorModified.Print(relPath)
		colorDelta.Print(" (", HumanSizeSign(val.Size-prevMap[key].Size), ")\n")
	}
	for _, key := range delList {
		relPath := strings.Replace(key, AbsPath(currDirInfo.Path), "", 1)
		val := prevMap[key] // use prev map (!)
		colorDeleted.Print("- ")
		colorDeleted.Printf("%-10s", HumanSize(val.Size))
		colorDeletedDirPart.Print(currDirInfo.Path)
		colorDeleted.Print(relPath)
		fmt.Println()
	}
}

func PrintTable(prevSnapshot, currSnapshot SnapshotStruct) {
	tableWriter := table.NewWriter()
	tableWriter.SetStyle(table.StyleRounded)
	tableWriter.SetOutputMirror(os.Stdout)
	tableWriter.AppendHeader(table.Row{"path", "size", "dirs", "files" /*"last modified",*/, "walk time"})

	for i, currDirInfo := range currSnapshot.infoList {
		prevDirInfo := prevSnapshot.infoList[i]

		var deltaSize, deltaDirs, deltaFiles string

		if prevDirInfo.Path != "" {
			if prevDirInfo.Size != currDirInfo.Size {
				deltaSize = " (" + HumanSizeSign(currDirInfo.Size-prevDirInfo.Size) + ")"
			}
			if prevDirInfo.Dirs != currDirInfo.Dirs {
				deltaDirs = " (" + fmt.Sprintf("%+d", currDirInfo.Dirs-prevDirInfo.Dirs) + ")"
			}
			if prevDirInfo.Files != currDirInfo.Files {
				deltaFiles = " (" + fmt.Sprintf("%+d", currDirInfo.Files-prevDirInfo.Files) + ")"
			}
		}

		tableWriter.AppendRow([]interface{}{
			currDirInfo.Path,
			HumanSize(currDirInfo.Size) + deltaSize,
			strconv.Itoa(currDirInfo.Dirs) + deltaDirs,
			strconv.Itoa(currDirInfo.Files) + deltaFiles,
			//currDirInfo.ModTime.Format(time.RFC822),
			currDirInfo.walkDuration,
		})
	}

	tableWriter.AppendSeparator()
	if prevSnapshot.FreeSpace > 0 {
		tableWriter.AppendRow(table.Row{ // print previous start time
			ColorHeader("prev stime (t₀)"),
			ColorPale(prevSnapshot.StartTime.Format("02 Jan 15:04")),
			ColorPale(TimeAgo(prevSnapshot.StartTime)),
		})
	}
	tableWriter.AppendRow(table.Row{
		ColorHeaderHi("start time (t₁)"),
		currSnapshot.StartTime.Format("02 Jan 15:04"),
		TimeAgo(currSnapshot.StartTime),
	})
	tableWriter.AppendSeparator()

	deltaFreeSpace := ""
	if prevSnapshot.FreeSpace != 0 && prevSnapshot.FreeSpace != currSnapshot.FreeSpace {
		deltaFreeSpace = " (" + HumanSizeSign(currSnapshot.FreeSpace-prevSnapshot.FreeSpace) + ")"
	}

	tableWriter.AppendRow(table.Row{
		"FREE SPACE",
		color.HiGreenString(HumanSize(currSnapshot.FreeSpace)) + deltaFreeSpace,
		"", "", /*"",*/
		time.Since(gStartTime).Round(time.Millisecond),
	})
	tableWriter.Render()

}

func main() {
	flag.Parse()

	InitLogger()
	InitConfig()
	InitDataDirs()
	InitStdoutSaver()

	var stepsBack = 0
	if *gRepLast {
		stepsBack = 1 // pre-previous
	}

	prevSnapshot := LoadPrevSnapshot(stepsBack)

	// calculate free space
	_freeSpace, _ := GetFreeSpace()

	var currSnapshot = SnapshotStruct{
		FreeSpace: _freeSpace,
		StartTime: gStartTime,
	}

	if *gRepLast {
		currSnapshot = LoadPrevSnapshot(0)
	}

	if !*gNoSave && !*gRepLast {
		SaveSnapshot(currSnapshot)
	}

	// for each directory
	for _, dir := range gCfg.Dirs {
		// load previous state of the directory
		prevDirInfo, _ := LoadPrevDirInfo(dir.Path, stepsBack)

		// calculate current state
		var currDirInfo DirInfoStruct
		start := time.Now()
		if *gRepLast {
			currDirInfo, _ = LoadPrevDirInfo(dir.Path, 0)
		} else {
			var err error
			currDirInfo, err = ProcessDirectory(dir.Path)
			if err != nil {
				LogErr(err)
				//continue
			}
		}
		currDirInfo.walkDuration = time.Since(start).Round(time.Millisecond)

		prevSnapshot.infoList = append(prevSnapshot.infoList, prevDirInfo)
		currSnapshot.infoList = append(currSnapshot.infoList, currDirInfo)

		if !*gNoSave && !*gRepLast {
			SaveDirInfo(dir.Path, currDirInfo)
		}

		// print diff
		if gCfg.DetailedMode {
			PrintDiff(prevDirInfo, currDirInfo)
		}
	} // dir loop

	// print result table
	PrintTable(prevSnapshot, currSnapshot)

	DeleteOldSnapshots()

	if *gDaemonMode {
		fmt.Println("Running in daemon mode..")
		for {
			time.Sleep(time.Millisecond * 100)
		}
	}
}
