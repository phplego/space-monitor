package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/fatih/color"
	"github.com/shirou/gopsutil/disk"
	"github.com/xeonx/timeago"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

var gTimeAgoConfig = timeago.Config{
	PastSuffix:   " ago",
	FuturePrefix: "in ",
	Periods: []timeago.FormatPeriod{
		{time.Second, "a sec", "%d sec"},
		{time.Minute, "a min", "%d min"},
		{time.Hour, "an hour", "%d hrs"},
		{timeago.Day, "one day", "%d days"},
		{timeago.Month, "one mon", "%d mons"},
		{timeago.Year, "one year", "%d yrs"},
	},
	Zero:          "moments",
	Max:           99 * timeago.Year,
	DefaultLayout: "2006-01-02",
}

func TimeAgo(t time.Time) string {
	return gTimeAgoConfig.Format(t)
}

func ColorHeader(str string, a ...interface{}) string {
	return color.New(color.FgBlue).Add(color.Bold).Sprintf(str, a...)
}

func ColorHeaderHi(str string, a ...interface{}) string {
	return color.New(color.FgHiBlue).Add(color.Bold).Sprintf(str, a...)
}

func ColorPale(str string, a ...interface{}) string {
	return color.New(color.FgHiYellow). /*.Add(color.Bold)*/ Sprintf(str, a...)
}

func Bold(str string, a ...interface{}) string {
	return color.New(color.Bold).Sprintf(str, a...)
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
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; abs(n) >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func HumanSizeSign(bytes int64) string {
	str := HumanSize(bytes)
	if !strings.HasPrefix(str, "-") {
		return "+" + str
	}
	return str
}

func GetFreeSpace() (int64, error) {

	di, err := disk.Usage(".")
	if err != nil {
		return 0, err
	}
	return int64(di.Free), nil

	//wd, err := os.Getwd()
	//var stat unix.Statfs_t
	//
	//err = unix.Statfs(wd, &stat)
	//if err != nil {
	//	return 0, err
	//}
	//
	//// Available blocks * Size per block = available space in bytes
	//return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func GetAppDir() string {
	path, _ := os.Executable()
	return filepath.Dir(path)
}

func GetHash(text string) string {
	//h := xxh3.HashString128(text)
	//return fmt.Sprintf("%x%x", h.Hi, h.Lo)
	hash := sha1.Sum([]byte(text))
	return hex.EncodeToString(hash[0:10]) // first half of SHA1 (10 bytes)
}

func AbsPath(path string) string {
	usr, _ := user.Current()

	if path == "~" {
		return usr.HomeDir
	} else if strings.HasPrefix(path, "~/") {
		return filepath.Join(usr.HomeDir, path[2:])
	}
	return path
}
