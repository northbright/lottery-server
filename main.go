// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/northbright/pathhelper"
)

type ServerSettings struct {
	WSURL string `json:"ws_url"`
}

var (
	serverRoot       string // Absolute path of server root.
	staticFolderPath string // Absolute path of static file folder.
	faviconPath      string // Absolute path of "favicon.ico".
)

var addr = flag.String("addr", ":8080", "http service address")

func loadServerSettings(file string, settings *ServerSettings) error {
	var (
		err        error
		buf        []byte
		currentDir string
	)

	currentDir, _ = pathhelper.GetCurrentExecDir()
	file = path.Join(currentDir, file)

	// Load Conifg
	if buf, err = ioutil.ReadFile(file); err != nil {
		return err
	}

	return json.Unmarshal(buf, &settings)
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

// GetCurrentExecDir gets the current executable path.
// You may find more path helper functions in:
// https://github.com/northbright/pathhelper
func GetCurrentExecDir() (dir string, err error) {
	p, err := exec.LookPath(os.Args[0])
	if err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}

	dir = filepath.Dir(absPath)
	return dir, nil
}

func init() {
	// Get absolute path of server root(current executable).
	serverRoot, _ = GetCurrentExecDir()
	// Get static folder path.
	staticFolderPath = path.Join(serverRoot, "./dist/spa")
	// Get favicon.ico path.
	faviconPath = path.Join(serverRoot, "favicon.ico")
	// Get index template file path.
}

func main() {
	var err error

	flag.Parse()

	settings := ServerSettings{}
	if err = loadServerSettings("server_settings.json", &settings); err != nil {
		fmt.Printf("loadServerSettings() error: %v\n", err)
		return
	}

	fmt.Printf("settings.WSURL: %v\n", settings.WSURL)

	if participants, err = loadParticipants(participantsCSV); err != nil {
		fmt.Printf("loadParticipants() error: %v\n", err)
		return
	}

	for _, p := range participants {
		fmt.Printf("id: %v, name: %v\n", p.ID, p.Name)
	}
	availParticipants = participants

	if err = loadConfig(configFile, &config); err != nil {
		fmt.Printf("loadConfig() error: %v\n", err)
		return
	}
	fmt.Printf("config: %v\n", config)

	// Serve Static Files
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir(staticFolderPath))))

	//http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(w, r)
	})

	http.HandleFunc("/get-ws-url/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(settings.WSURL))
	})

	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
