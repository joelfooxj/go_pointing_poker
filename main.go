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
	// TODO: Should wrap the globalMap in a struct
	// so we can hook any
	// changes to the map to hooks
	globalMap = make(map[string]int)
	mu        sync.Mutex

	//go:embed templates/*.tmpl.html
	files      embed.FS
	mainPage   *template.Template
	loginPage  *template.Template
	globalChan = make(chan bool)
)

/*
Notes:
Does it make sense to encapsulate a client, channel, routine and
map key into a struct/entity?

This way, we can hide the implementation details from the handler

I want there to be a way to isolate the flows of each of client
so that one client can never be coupled to another

Each client would then only have modify access to their specific key
in the global map. We can say that their modifications are by definition
isolated since each client has their own key -> their own memory address.

TODO:
** Implement Admin functionality **
A special user called TBADMIN that can do the following things:
- Reset all keys
- Does not have a key, but receives updates




*/

type Broker struct {
	// The idomatic way of implementing a set is a map
	// This stores the set of active clients
	clients map[chan bool]bool

	// A channel to recieve the channels of
	// new clients to be stored in the set/map
	newClients chan chan bool

	// A channel to receive the channels of disconnected clients
	// to be removed from the set/map
	dcClients chan chan bool
}

// Broker should not be involved in modifying the global map
// only to relay the update signal to the clients
func (b *Broker) Start() {
	go func() {
		for {
			// Block until we receive from one of the
			// three following channels.
			select {
			case s := <-b.newClients:
				// A new client has connected.
				// Store the new client
				b.clients[s] = true
				// globalChan <- true
				log.Println("Added new client")

			case s := <-b.dcClients:
				// A client has disconnected.
				// Remove the client from the set/map
				delete(b.clients, s)
				close(s)
				// globalChan <- true
				log.Println("Removed client")

			case hasUpdate := <-globalChan:
				// Iterate through and relay the update signal to
				// each client channel
				for s := range b.clients {
					s <- hasUpdate
				}
				log.Printf("Notifying %d clients", len(b.clients))
			}
		}
	}()
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

// func removeKeyHandler(w http.ResponseWriter, req *http.Request) {
// 	if req.Method != "DELETE" {
// 		w.WriteHeader(http.StatusMethodNotAllowed)
// 		return
// 	}
// 	key := req.URL.Query().Get("key")

// 	mu.Lock()
// 	delete(globalMap, key)
// 	mu.Unlock()

// 	globalChan <- true

// 	w.WriteHeader(http.StatusOK)
// 	w.Header().Set("Content-Type", "application/json")
// }

func serveMainPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving main page")

	// Grab and set the key
	key := req.URL.Query().Get("username")

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

// Opens the connection with client
// and remains open until client closes connection
func (b *Broker) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Make sure that the writer supports flushing.
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	key := req.URL.Query().Get("username")
	fmt.Println("Serving key:", key)

	mu.Lock()
	globalMap[key] = 0
	mu.Unlock()

	clientChan := make(chan bool)
	b.newClients <- clientChan

	// Listen to the closing of the http connection via the CloseNotifier
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		// Remove this client from the map of attached clients
		// when `EventHandler` exits.
		delete(globalMap, key)
		globalChan <- true

		b.dcClients <- clientChan
		log.Println(key, "just closed.")
	}()

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	payload, err := json.Marshal(globalMap)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(w, "data: %s\n\n", payload)
	f.Flush()

	globalChan <- true

	// Don't close the connection, instead loop endlessly.
	for {
		// Notify the routine that globalMap has been updated
		_, open := <-clientChan

		if !open {
			// If our messageChan was closed, this means that the client has
			// disconnected.
			fmt.Println("Client disconnected")
			break
		}

		fmt.Println("Sending update to client", globalMap)

		// Marshall the globalMap to a json
		payload, err := json.Marshal(globalMap)
		if err != nil {
			panic(err)
		}

		// Write to the ResponseWriter, `w`.
		fmt.Fprintf(w, "data: %s\n\n", payload)

		// Flush the response.  This is only possible if
		// the response supports streaming.
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

	broker := &Broker{
		make(map[chan bool]bool),
		make(chan (chan bool)),
		make(chan (chan bool)),
	}

	// Broker starts listening and relaying signals
	broker.Start()

	http.HandleFunc("/setkey", setKeyHandler)
	http.HandleFunc("/resetallkeys", resetAllKeysHandler)
	// http.HandleFunc("/removekey", removeKeyHandler)
	http.HandleFunc("/main", serveMainPageHandler)
	http.HandleFunc("/", loginPageHandler)
	http.Handle("/sse_events", broker)

	http.ListenAndServe(":8090", nil)
}
