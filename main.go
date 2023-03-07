// Code modified from https://github.com/kljensen/golang-html5-sse-example

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

var (
	globalMap = make(map[string]int)
	mu        sync.Mutex

	//go:embed templates/*.tmpl.html
	files      embed.FS
	mainPage   *template.Template
	loginPage  *template.Template
	globalChan = make(chan bool)
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

	fmt.Println("Checking if", key, "is in map...")

	_, ok := globalMap[key]

	if ok {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("true"))
	} else {
		w.WriteHeader(http.StatusOK)
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

	fmt.Println("setkey received:", data)

	key := data["key"].(string)
	value := int(data["value"].(float64))

	// Might not have to lock inside mutex since only one routine
	// should be assigned to a key...
	mu.Lock()
	globalMap[key] = value
	mu.Unlock()

	fmt.Println("globalMap has been updated to", globalMap)

	globalChan <- true

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
}

func registerKeyAndLoginHandler(w http.ResponseWriter, req *http.Request) {
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

	fmt.Println("setkey received:", data)

	key := data["key"].(string)
	value := int(data["value"].(float64))

	// Might not have to lock inside mutex since only one routine
	// should be assigned to a key...
	globalMap[key] = value
	globalChan <- true

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
}

func resetAllKeysHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("Resetting all keys")

	mu.Lock()
	for key := range globalMap {
		globalMap[key] = 0
	}
	mu.Unlock()

	globalChan <- true

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
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

	globalChan <- true

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
}

func serveMainPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving main page")

	// Grab and set the key
	key := req.URL.Query().Get("key")

	mu.Lock()
	globalMap[key] = 0
	mu.Unlock()

	if err := mainPage.Execute(w, key); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func loginPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving login page")

	if err := loginPage.Execute(w, nil); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func serveSSEHandler(w http.ResponseWriter, req *http.Request) {
	// Make sure that the writer supports flushing.
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// // Create a new channel, over which the broker can
	// // send this client messages.
	// messageChan := make(chan string)

	// // Add this client to the map of those that should
	// // receive updates
	// b.newClients <- messageChan

	// Listen to the closing of the http connection via the CloseNotifier
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		// Remove this client from the map of attached clients
		// when `EventHandler` exits.
		// b.defunctClients <- messageChan
		log.Println("HTTP connection just closed.")
	}()

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	fmt.Println("client subscribed to SSE")

	payload, err := json.Marshal(globalMap)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(w, "data: %s\n\n", payload)
	f.Flush()

	// Don't close the connection, instead loop endlessly.
	for {
		// Notify the routine that globalMap has been updated
		_, open := <-globalChan

		fmt.Println("Received update from globalChan")

		if !open {
			// If our messageChan was closed, this means that the client has
			// disconnected.
			break
		}

		fmt.Println("Sending update to client", globalMap)

		// // Marshall the globalMap to a json
		payload, err := json.Marshal(globalMap)
		if err != nil {
			panic(err)
		}

		// Write to the ResponseWriter, `w`.
		fmt.Fprintf(w, "data: %s\n\n", payload)

		// Flush the response.  This is only possible if
		// the repsonse supports streaming.
		f.Flush()
	}
}

func main() {
	page1, err := template.ParseFS(files, "templates/main_page.tmpl.html")
	if err != nil {
		panic(err)
	}
	mainPage = page1

	page2, err := template.ParseFS(files, "templates/login_page.tmpl.html")
	if err != nil {
		panic(err)
	}
	loginPage = page2

	http.HandleFunc("/getmap", getMapHandler)
	http.HandleFunc("/iskeyinmap", isKeyInMapHandler)
	http.HandleFunc("/setkey", setKeyHandler)
	http.HandleFunc("/resetallkeys", resetAllKeysHandler)
	http.HandleFunc("/removekey", removeKeyHandler)
	http.HandleFunc("/main", serveMainPageHandler)
	http.HandleFunc("/login", loginPageHandler)
	http.HandleFunc("/sse_events", serveSSEHandler)

	http.ListenAndServe(":8090", nil)
}
