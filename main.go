package main

import (
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"github.com/acarl005/stripansi"
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
	runtime "runtime"
	"sort"
	"space-monitor/libs/fmt2"
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
	gConfigFile = flag.String("config", "config.yaml", "Config file")

	// paths and files
	gDataDir = GetAppDir() + "/data"
)

// Config is an application configuration structure
type Config struct {
	Dirs         []Config_DirectorySettings `yaml:"dirs"`
	MaxSnapshots int                        `yaml:"max-snapshots"`
	DetailedMode bool                       `yaml:"detailed-mode"`
}

// Config_DirectorySettings directory settings (path etc.)
type Config_DirectorySettings struct {
	Path string `yaml:"path"`
}

// DirInfoStruct contains all collected information about directory during the scan
type DirInfoStruct struct {
	Path         string    `yaml:"path"`
	Size         int64     `yaml:"size"`
	Files        int       `yaml:"files"`
	Dirs         int       `yaml:"dirs"`
	StartTime    time.Time `yaml:"stime"` // the time when the scan was started
	walkDuration time.Duration
	fileMap      map[string]GobFileInfo // file details for detailed mode
}

// GobFileInfo - file info structure saved to the .gob file
type GobFileInfo struct {
	IsDir bool
	Size  int64
}

type ChangeType int

const (
	ADDED ChangeType = iota
	MODIFIED
	DELETED
)

type Change struct {
	path       string
	changeType ChangeType
	gob        GobFileInfo
	deltaSize  int64
}

type SnapshotStruct struct {
	FreeSpace int64     `yaml:"free-space"`
	StartTime time.Time `yaml:"start-time"`
	infoList  []DirInfoStruct
}

func GetConfigFileAbs() string {
	// check if config file path is absolute
	if filepath.IsAbs(*gConfigFile) {
		return *gConfigFile
	} else {
		return GetAppDir() + "/" + *gConfigFile // prepend with AppDir
	}
}

func LogErr(v ...any) {
	gLogger.Println(v...)
	// noinspection GoUnhandledErrorResult
	color.New(color.FgHiRed, color.Italic).Println(v...)
}

// GetSnapshotDirectory return current snapshot directory
func GetSnapshotDirectory() string {
	switch {
	// noinspection GoBoolExpressions
	case runtime.GOOS == "windows":
		return gDataDir + "/" + gStartTime.Format("2006-01-02 15_04_05")
	default:
		return gDataDir + "/" + gStartTime.Format("2006-01-02 15:04:05")
	}
}

func InitLogger() {
	var logFilename = GetAppDir() + "/space-monitor.log"
	file, err := os.OpenFile(logFilename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		LogErr("error opening file:", err)
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
	err := cleanenv.ReadConfig(GetConfigFileAbs(), &gCfg)
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
	reportFlBW, _ := os.OpenFile(GetSnapshotDirectory()+"/report-bw.txt", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	bwWriter := FilterFunc(reportFlBW, func(bytes []byte) []byte {
		str := stripansi.Strip(string(bytes))
		return []byte(str)
	})
	multiWriter := io.MultiWriter(os.Stdout, bwWriter, reportFile)
	color.Output = multiWriter
	fmt2.OutWriter = multiWriter
}

// SaveDirInfo saves struct to .dat (yaml) and .gob (filemap) files
func SaveDirInfo(dirInfo DirInfoStruct) {
	pathHash := GetHash(dirInfo.Path)
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

// LoadPrevDirInfo loads previous dir info struct from previous snapshot directory
func LoadPrevDirInfo(dir string, stepsBack int) (DirInfoStruct, error) {
	files, _ := filepath.Glob(gDataDir + fmt.Sprintf("/*/dirinfo-%s.dat", GetHash(AbsPath(dir))))
	if files == nil {
		fmt2.Println("no dirinfo files for", color.BlueString(dir), "is it first run?")
		return DirInfoStruct{}, errors.New("no prev dirinfo files")
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		return DirInfoStruct{}, errors.New("out of bounds dirinfo array. index=" + strconv.Itoa(index))
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

// ProcessDirectory collects full directory information
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
				info.fileMap[path] = GobFileInfo{IsDir: fileInfo.IsDir(), Size: size}

				// increment size of all parent dirs
				current := filepath.Dir(path)
				for strings.HasPrefix(current, dir) {
					//fmt2.Println("current:", current)
					if gobInfo, ok := info.fileMap[current]; ok {
						gobInfo.Size += size
						info.fileMap[current] = gobInfo
					} else {
						LogErr("Unknown parent:", current, "for file:", path)
					}
					current = filepath.Dir(current)
				}
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
		LogErr("error opening snapshot file: ", err)
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
		fmt2.Println("no snapshot files; is it first run?")
		return SnapshotStruct{}
	}
	sort.Strings(files)
	index := len(files) - 1 - stepsBack
	if index < 0 || index >= len(files) {
		LogErr("out of bounds snapshot array. len(files):", len(files), "stepsBack:", stepsBack)
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
		return // MaxSnapshots not exceeded. No need to delete
	}
	sort.Slice(dirs, func(i, j int) bool { // sort dirs (older first)
		return strings.Compare(dirs[i].Name(), dirs[j].Name()) < 0
	})

	// delete first (len(dirs) - MaxSnapshots) folders
	for _, dir := range dirs[0 : len(dirs)-gCfg.MaxSnapshots] {
		err := os.RemoveAll(gDataDir + "/" + dir.Name())
		if err != nil {
			LogErr(err)
			return
		}
	}
}

// Diff collects and returns Change list
func Diff(prevDirInfo, currDirInfo DirInfoStruct) []Change {
	var prevMap = prevDirInfo.fileMap
	var currMap = currDirInfo.fileMap

	if len(prevMap) == 0 {
		return []Change{} // skip empty map (eg. when first run)
	}

	var changes []Change // all changes will be collected here

	for key := range currMap {
		if _, ok := prevMap[key]; !ok { // exists in current and doesn't exist in prev - means new file ADDED
			changes = append(changes, Change{key, ADDED, currMap[key], currMap[key].Size})
		} else {
			if currMap[key].Size != prevMap[key].Size { // exists in both but size is changed - means file MODIFIED
				changes = append(changes, Change{key, MODIFIED, currMap[key], currMap[key].Size - prevMap[key].Size})
			}
		}
	}
	for key := range prevMap {
		if _, ok := currMap[key]; !ok { // exists in previous and doesn't exist in current - means file DELETED
			changes = append(changes, Change{key, DELETED, prevMap[key], 0 - prevMap[key].Size})
		}
	}

	sort.Slice(changes, func(i, j int) bool { // sort changes by path
		return strings.Compare(changes[i].path, changes[j].path) < 0
	})
	return changes
}

// PrintDiff calculates and prints directory structure changes
// noinspection GoUnhandledErrorResult
func PrintDiff(prevDirInfo, currDirInfo DirInfoStruct) {
	colorAddMain := color.New(color.FgHiGreen)
	colorAddInvr := color.New(color.BgHiGreen, color.FgBlack)
	colorModMain := color.New(color.FgHiBlue)
	colorModInvr := color.New(color.BgHiBlue, color.FgBlack)
	colorDelMain := color.New(color.FgHiRed)
	colorDelInvr := color.New(color.BgHiRed, color.FgBlack)
	colorDeltaSz := color.New(color.FgHiMagenta)

	changes := Diff(prevDirInfo, currDirInfo)
	if len(changes) == 0 {
		return
	}

	color.New(color.Bold, color.FgWhite).Printf("\n\nDiff of %s:\n\n", currDirInfo.Path)

	for _, change := range changes {
		relPath := strings.Replace(change.path, AbsPath(currDirInfo.Path), "", 1)
		var colMain, colInvr *color.Color
		var symbol, icon string
		switch change.changeType {
		case ADDED:
			symbol = "+"
			colMain = colorAddMain
			colInvr = colorAddInvr
		case MODIFIED:
			symbol = "↗"
			if change.deltaSize < 0 {
				symbol = "↘"
			}
			colMain = colorModMain
			colInvr = colorModInvr
		case DELETED:
			symbol = "-"
			colMain = colorDelMain
			colInvr = colorDelInvr
		}

		switch change.gob.IsDir {
		case true:
			icon = "🗀"
		case false:
			icon = ""
		}

		colMain.Printf(" %-2s ", icon)
		colMain.Printf("%-2s", symbol)
		colMain.Printf("%-10s", HumanSize(change.gob.Size))
		if change.changeType == MODIFIED {
			colorDeltaSz.Printf("%-11s", HumanSizeSign(change.deltaSize))
		} else {
			colorDeltaSz.Printf("%-11s", "")
		}
		colInvr.Print(AbsPath(currDirInfo.Path))
		colMain.Print(relPath)
		colMain.Println()
	}
}

// PrintTable prints summarized table
func PrintTable(prevSnapshot, currSnapshot SnapshotStruct) {
	tableWriter := table.NewWriter()
	tableWriter.SetTitle("%d directories", len(currSnapshot.infoList))
	tableWriter.SetStyle(table.StyleRounded)
	tableWriter.SetOutputMirror(fmt2.OutWriter)
	tableWriter.AppendHeader(table.Row{"path", "size", "dirs", "files", "walk time"})

	for i, currDirInfo := range currSnapshot.infoList {
		prevDirInfo := prevSnapshot.infoList[i]

		var deltaSize, deltaDirs, deltaFiles string

		if prevDirInfo.Path != "" {
			if prevDirInfo.Size != currDirInfo.Size {
				deltaSize = " " + HumanSizeSign(currDirInfo.Size-prevDirInfo.Size)
			}
			if prevDirInfo.Dirs != currDirInfo.Dirs {
				deltaDirs = " (" + fmt.Sprintf("%+d", currDirInfo.Dirs-prevDirInfo.Dirs) + ")"
			}
			if prevDirInfo.Files != currDirInfo.Files {
				deltaFiles = " (" + fmt.Sprintf("%+d", currDirInfo.Files-prevDirInfo.Files) + ")"
			}
		}

		tableWriter.AppendRow([]interface{}{
			color.HiBlueString(shorifyPath(currDirInfo.Path)),
			HumanSize(currDirInfo.Size) + color.HiMagentaString(deltaSize),
			strconv.Itoa(currDirInfo.Dirs) + deltaDirs,
			strconv.Itoa(currDirInfo.Files) + deltaFiles,
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
		ColorHeaderHi("curr stime (t₁)"),
		currSnapshot.StartTime.Format("02 Jan 15:04"),
		TimeAgo(currSnapshot.StartTime),
	})
	tableWriter.AppendSeparator()

	deltaFreeSpace := ""
	if prevSnapshot.FreeSpace != 0 && prevSnapshot.FreeSpace != currSnapshot.FreeSpace {
		deltaFreeSpace = " " + HumanSizeSign(currSnapshot.FreeSpace-prevSnapshot.FreeSpace)
	}

	tableWriter.AppendRow(table.Row{
		"FREE SPACE",
		color.HiGreenString(HumanSize(currSnapshot.FreeSpace)) + color.HiMagentaString(deltaFreeSpace),
		"", "",
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

	fmt2.Println()
	fmt2.Println(" SPACE MONITOR")
	fmt2.Print(" config: ")
	color.New(color.BgHiBlack).Print(" " + GetConfigFileAbs() + " ")

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
			SaveDirInfo(currDirInfo)
		}

		// print diff
		if gCfg.DetailedMode {
			PrintDiff(prevDirInfo, currDirInfo)
		}
	} // dir loop

	// print result table
	fmt2.Println()
	PrintTable(prevSnapshot, currSnapshot)

	fmt.Println()

	DeleteOldSnapshots()

	//width, height, _ := terminal.GetSize(0)
	//fmt2.Println("size", width, height)

	if *gDaemonMode {
		fmt2.Println("Running in daemon mode..")
		for {
			time.Sleep(time.Millisecond * 100)
		}
	}
}
