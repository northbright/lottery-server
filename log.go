package main

import (
	"os"
	"path"

	"github.com/northbright/pathhelper"
)

var (
	logFile = `log.txt`
)

func writeLog(buf []byte) error {
	currentDir, _ := pathhelper.GetCurrentExecDir()
	p := path.Join(currentDir, logFile)

	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.Write(buf); err != nil {
		return err
	}

	return nil
}
