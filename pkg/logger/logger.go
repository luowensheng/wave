package logger

import (
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
	"easyserver/pkg/common"
	"easyserver/pkg/logger/loglevel"
	"time"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorReset  = "\033[0m"
)

var logfile *os.File = nil
var lastOutputPath string
var lastDate = time.Now()
var _LOG_LEVEL = loglevel.TRACE


var log_output_dir_path = "./logs"
var log_output_file_name = "log"

var printFileInfo = os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"

func toString2Digits(value int) string {
	strValue := fmt.Sprint(value)
	if len(strValue) == 1 {
		strValue = "0" + strValue
	}
	return strValue
}

func getOrCreateCurrentLogFileLocation() (string, error) {

	now := time.Now()
	year := now.Year()
	month := toString2Digits(int(now.Month()))
	day := toString2Digits(now.Day())

	paths := []string{}
	paths = append(paths, fmt.Sprint(year), fmt.Sprint(month))

	outDir := log_output_dir_path

	for i := range paths {

		outDir = path.Join(outDir, paths[i])

		if !pathExists(outDir) {
			if err := os.Mkdir(outDir, os.FileMode(0777)); err != nil {
				return "", err
			} else {
				fmt.Printf("CREATED NEW PATH :%v", outDir)
			}
		}
	}

	return path.Join(outDir, fmt.Sprintf("%s-%v-%v-%v.log", log_output_file_name, year, month, day)), nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func SetPrintFileInfo(shouldPrintFileInfo bool) {
	printFileInfo = shouldPrintFileInfo
}

func SetOutputFileData(directoryPath, filename string) {
	log_output_dir_path = directoryPath
	log_output_file_name = filename
}

func dateHasChanged() bool {

	now := time.Now()

	isDifferentDay := now.Day() != lastDate.Day() || now.Year() != lastDate.Year()

	if isDifferentDay {
		lastDate = now
	}
	return isDifferentDay
}

func logContent(logLevel loglevel.LOG_LEVEL, format string, v ...any) {

	if dateHasChanged() || logfile == nil {

		output_path, err := getOrCreateCurrentLogFileLocation()
		if err == nil {
			if lastOutputPath != output_path && logfile != nil {
				logfile.Close()
			}
			logfile, err = os.OpenFile(output_path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatal(err)
			}
			lastOutputPath = output_path
			log.SetOutput(logfile)
		}
	}

	pc, _, _, _ := runtime.Caller(2)
	funcInfo := runtime.FuncForPC(pc)

	filepath, line := funcInfo.FileLine(pc)

	var formattedContent string
	if printFileInfo {
		formattedContent = fmt.Sprintf("%s:%v (%s) %s", filepath, line, funcInfo.Name(), fmt.Sprintf(format, v...))

	} else {
		formattedContent = fmt.Sprintf(format, v...)
	}

	currentTime := time.Now().Format("2006-01-02 15:04:05")

	// logRequest <-
	// log.Printf("[%v] %v\n", logLevel, formattedContent)
	// printRequest <-

	// if logLevel.GetIndex() >= _LOG_LEVEL.GetIndex() {
	fmt.Printf("%s%s%s [%s%s%s] %s\n\n\n\n", colorBlue, currentTime, colorReset, levelColor(string(logLevel)), logLevel, colorReset, formattedContent)
	// }
}

func Debug(v ...any) {
	logContent(loglevel.DEBUG, "%s", fmt.Sprint(v...))
}

func Trace(v ...any) {
	logContent(loglevel.TRACE, "%s", fmt.Sprint(v...))
}

func Warn(v ...any) {
	logContent(loglevel.WARN, "%s", fmt.Sprint(v...))
}

func Fatal(v ...any) {
	if printFileInfo {
		fmt.Println(string(common.GetStackTrace()))
	}
	logContent(loglevel.Fatal, "%s", fmt.Sprint(v...))
	os.Exit(1)
}

func Info(v ...any) {
	logContent(loglevel.INFO, "%s", fmt.Sprint(v...))
}

func Error(v ...any) {
	logContent(loglevel.ERROR, "%s", fmt.Sprint(v...))
}

func SetLogLevel(logLevel loglevel.LOG_LEVEL) {
	_LOG_LEVEL = logLevel
}

func Debugf(format string, v ...any) {
	logContent(loglevel.DEBUG, format, v...)
}

func Tracef(format string, v ...any) {

	logContent(loglevel.TRACE, format, v...)
	fmt.Println(string(common.GetStackTrace()))
}

func Warnf(format string, v ...any) {
	logContent(loglevel.WARN, format, v...)
}

func Fatalf(format string, v ...any) {
	logContent(loglevel.Fatal, format, v...)
	os.Exit(1)
}

func Infof(format string, v ...any) {
	logContent(loglevel.INFO, format, v...)
}

func Errorf(format string, v ...any) {
	logContent(loglevel.ERROR, format, v...)
}

func levelColor(level string) string {
	switch level {
	case "TRACE":
		return colorYellow
	case "DEBUG":
		return colorGreen
	case "INFO":
		return colorGreen
	case "WARN":
		return colorYellow
	case "ERROR":
		return colorRed
	case "FATAL":
		return colorRed
	default:
		return colorReset
	}
}
