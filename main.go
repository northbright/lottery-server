// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var addr = flag.String("addr", ":8081", "http service address")

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

func main() {
	var err error

	flag.Parse()

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

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(w, r)
	})
	err = http.ListenAndServe(*addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
