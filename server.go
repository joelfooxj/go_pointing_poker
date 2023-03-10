// Code modified from https://github.com/kljensen/golang-html5-sse-example
/*
Architecture:

The overall design is basically for multiple client to
update the map of keys, and subscribe to updates to the map.

There are 2 main components:
1. The mapManager, which provides an interface to modify/access the map
2. A broker manages the channels for clients

We wrap the map in an interface because we toggle between
a visible map and a hidden map. We also want to automatically
push any map updates to the clients.

*/

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

const TBADMIN = "TBADMIN"

var (
	//go:embed templates/*.tmpl.html
	files     embed.FS
	mainPage  *template.Template
	loginPage *template.Template

	mapManager = &MapManager{
		mu:           sync.Mutex{},
		pointsMap:    make(map[string]int),
		hiddenMap:    make(map[string]string),
		updateChan:   make(chan bool),
		isMapVisible: true,
	}

	broker = &Broker{
		make(map[chan bool]bool),
		make(chan (chan bool)),
		make(chan (chan bool)),
	}

	isTBAdminLoggedIn = false
)

type MapManager struct {
	mu           sync.Mutex
	pointsMap    map[string]int
	hiddenMap    map[string]string
	updateChan   chan bool
	isMapVisible bool
}

func (m *MapManager) setMapVisibility(isVisible bool) {
	m.isMapVisible = isVisible
	m.updateChan <- true
}

func (m *MapManager) toggleMapVisibility() {
	m.isMapVisible = !(m.isMapVisible)
	m.updateChan <- true
}

func (m *MapManager) getMapPayload() []byte {
	var payload []byte
	var err error
	if m.isMapVisible {
		payload, err = json.Marshal(mapManager.pointsMap)
		if err != nil {
			panic(err)
		}
	} else {
		payload, err = json.Marshal(mapManager.hiddenMap)
		if err != nil {
			panic(err)
		}
	}
	return payload
}

func (m *MapManager) resetMap() {
	m.mu.Lock()
	for key := range m.pointsMap {
		m.pointsMap[key] = 0
		m.hiddenMap[key] = ""
	}
	m.isMapVisible = false
	m.mu.Unlock()
	m.updateChan <- true
}

func (m *MapManager) keyExists(key string) bool {
	_, ok := m.pointsMap[key]
	return ok
}

func (m *MapManager) deleteKey(key string) {
	m.mu.Lock()
	delete(m.pointsMap, key)
	delete(m.hiddenMap, key)
	m.mu.Unlock()
	m.updateChan <- true
}

func (m *MapManager) addKey(key string) {
	m.mu.Lock()
	m.pointsMap[key] = 0
	m.hiddenMap[key] = ""
	m.mu.Unlock()
	m.updateChan <- true
}

func (m *MapManager) setKey(key string, value int) {
	m.mu.Lock()
	m.pointsMap[key] = value
	m.hiddenMap[key] = "?"
	m.mu.Unlock()
	m.updateChan <- true
}

type Broker struct {
	// The idomatic way of implementing a set is a map
	// This stores the set of active clients
	clients map[chan bool]bool

	// A channel to receive the channels of
	// new clients to be stored in the active set/map
	newClients chan chan bool

	// A channel to receive the channels of disconnected clients
	// to be removed from the active set/map
	dcClients chan chan bool
}

func (b *Broker) Start() {
	go func() {
		for {
			// Block until we receive from one of the
			// three following channels.
			select {
			case c := <-b.newClients:
				// A new client has connected.
				// Store the new client
				b.clients[c] = true
			case c := <-b.dcClients:
				// A client has disconnected.
				// Remove the client from the set/map
				delete(b.clients, c)
				close(c)
			case hasUpdate := <-mapManager.updateChan:
				// Iterate through and relay the update signal to
				// each client channel
				log.Printf("Notifying %d clients", len(b.clients))
				for c := range b.clients {
					c <- hasUpdate
				}
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

	key := data["key"].(string)
	value := int(data["value"].(float64))
	mapManager.setKey(key, value)

	w.WriteHeader(http.StatusOK)
}

func resetAllKeysHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("Resetting all keys")
	mapManager.resetMap()

	w.WriteHeader(http.StatusOK)
}

func toggleKeyVisibilityHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	fmt.Println("Toggling key visibility")
	mapManager.toggleMapVisibility()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("true"))
}

func serveMainPageHandler(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("username")

	forbiddenTBAadmin := key == TBADMIN && isTBAdminLoggedIn
	emptyKey := key == ""

	if forbiddenTBAadmin || emptyKey || mapManager.keyExists(key) {
		http.Redirect(w, req, "/", http.StatusForbidden)
		return
	}

	fmt.Println("Serving main page for user:", key)
	if err := mainPage.Execute(w, key); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func loginPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving login page for", req.Host)

	if err := loginPage.Execute(w, nil); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

// Opens the connection with client
// and remains open until client closes connection
func (b *Broker) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Make sure that the writer supports flushing.
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	clientChan := make(chan bool)
	b.newClients <- clientChan

	key := req.URL.Query().Get("username")
	fmt.Println("Subscribed:", key)

	if key == TBADMIN {
		isTBAdminLoggedIn = true
		mapManager.setMapVisibility(false)
	} else {
		mapManager.addKey(key)
	}

	payload := mapManager.getMapPayload()
	fmt.Fprintf(w, "data: %s\n\n", payload)
	f.Flush()

	// Listen to the closing of the http connection via the CloseNotifier
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		fmt.Println(key, "has disconnected")

		if isTBAdminLoggedIn && key == TBADMIN {
			isTBAdminLoggedIn = false
			mapManager.setMapVisibility(true)
		} else {
			mapManager.deleteKey(key)
		}
		b.dcClients <- clientChan
	}()

	// Don't close the connection, instead loop endlessly.
	for {
		// Notify the client via its routine
		// that there is an update
		_, open := <-clientChan

		if !open {
			fmt.Println("Channel for", key, "is closed")
			break
		}

		payload := mapManager.getMapPayload()
		fmt.Fprintf(w, "data: %s\n\n", payload)
		f.Flush()
	}
}

func main() {
	var err error
	mainPage, err = template.ParseFS(files, "templates/main_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	loginPage, err = template.ParseFS(files, "templates/login_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	// Broker starts listening and managing channels
	broker.Start()

	http.HandleFunc("/setkey", setKeyHandler)
	http.HandleFunc("/togglekeyvisibility", toggleKeyVisibilityHandler)
	http.HandleFunc("/resetkeys", resetAllKeysHandler)
	http.HandleFunc("/main", serveMainPageHandler)
	http.HandleFunc("/", loginPageHandler)
	http.Handle("/sse_events", broker)

	http.ListenAndServe(":8090", nil)
}
