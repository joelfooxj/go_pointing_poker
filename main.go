package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
)

// NOTE
// First line in the template file must be <!DOCTYPE html>?

var (
	globalMap = make(map[string]int)
	mu        sync.Mutex

	//go:embed templates/*.html
	files    embed.FS
	mainPage *template.Template
)

func getMapHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	jsonMap, _ := json.Marshal(globalMap)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonMap)

}

func isKeyInMapHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	key := req.URL.Query().Get("key")
	mu.Lock()
	_, ok := globalMap[key]
	mu.Unlock()
	if ok {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("true"))
	} else {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("false"))
	}
}

type setKeyData struct {
	Key   string `json:"key"`
	Value int    `json:"value"`
}

func setKeyHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var data map[string]interface{}

	rawBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(rawBytes, &data); err != nil {
		panic(err)
	}

	fmt.Println("The key is set with:", data)

	key := data["key"].(string)
	value := int(data["value"].(float64))

	globalMap[key] = value
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	jsonMap, _ := json.Marshal(globalMap)
	w.Write(jsonMap)
}

func resetAllKeysHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	mu.Lock()
	for key := range globalMap {
		globalMap[key] = 0
	}
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	jsonMap, _ := json.Marshal(globalMap)
	w.Write(jsonMap)
}

func removeKeyHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "DELETE" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key := req.URL.Query().Get("key")
	mu.Lock()
	delete(globalMap, key)
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	jsonMap, _ := json.Marshal(globalMap)
	w.Write(jsonMap)
}

// add SSE to the page
func serveMainPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving main page")

	if err := mainPage.Execute(w, globalMap); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func main() {
	// example init of the map
	globalMap["key1"] = 1
	globalMap["key2"] = 2

	pt, err := template.ParseFS(files, "templates/main_page.html")
	if err != nil {
		panic(err)
	}
	mainPage = pt

	http.HandleFunc("/getmap", getMapHandler)
	http.HandleFunc("/iskeyinmap", isKeyInMapHandler)
	http.HandleFunc("/setkey", setKeyHandler)
	http.HandleFunc("/resetallkeys", resetAllKeysHandler)
	http.HandleFunc("/removekey", removeKeyHandler)
	http.HandleFunc("/main", serveMainPageHandler)

	http.ListenAndServe(":8090", nil)
}
