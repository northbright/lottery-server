package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"time"

	"github.com/northbright/pathhelper"
)

func getLogFileName() string {
	t := time.Now()
	fileName := fmt.Sprintf("%02d-%02d-%02d.txt", t.Hour(), t.Minute(), t.Second())
	currentDir, _ := pathhelper.GetCurrentExecDir()
	p := path.Join(currentDir, fileName)
	return p
}

func logResponse(res interface{}) error {
	p := getLogFileName()

	buf, err := json.Marshal(res)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(p, buf, 0644)
}
