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
	"strings"
	"sync"

	"github.com/google/uuid"
)

const TBADMIN = "TBADMIN"
const MAX_USERS = 20

// Global vars
var (
	//go:embed templates/*.tmpl.html
	files       embed.FS
	mainPage    *template.Template
	landingPage *template.Template
	loginPage   *template.Template

	roomMap = make(map[string]*RoomManager)
)

type Broker struct {
	// The idomatic way of implementing a Set is as keys to a Map
	// This stores the set of active clients
	clients map[chan bool]bool

	// A channel to receive the channels of
	// new clients to be stored in the active set/map
	newClients chan chan bool

	// A channel to receive the channels of disconnected clients
	// to be removed from the active set/map
	dcClients chan chan bool

	// A channel to receive point updates for the room
	updateChan chan bool
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
			case hasUpdate := <-b.updateChan:
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

type RoomManager struct {
	mu                sync.Mutex
	pointsMap         map[string]string
	hiddenMap         map[string]string
	isMapVisible      bool
	isTBAdminLoggedIn bool
	adminHash         string
	broker            *Broker
}

func (rm *RoomManager) setPointsVisibility(isVisible bool) {
	rm.isMapVisible = isVisible
	rm.broker.updateChan <- true
}

func (rm *RoomManager) togglePointsVisibility() {
	rm.isMapVisible = !(rm.isMapVisible)
	rm.broker.updateChan <- true
}

func (rm *RoomManager) getPointsPayload() []byte {
	var payload []byte
	var err error
	if rm.isMapVisible {
		payload, err = json.Marshal(rm.pointsMap)
		if err != nil {
			panic(err)
		}
	} else {
		payload, err = json.Marshal(rm.hiddenMap)
		if err != nil {
			panic(err)
		}
	}
	return payload
}

func (rm *RoomManager) resetPoints() {
	rm.mu.Lock()
	for key := range rm.pointsMap {
		rm.pointsMap[key] = "0"
		rm.hiddenMap[key] = ""
	}
	rm.isMapVisible = false
	rm.mu.Unlock()
	rm.broker.updateChan <- true
}

func (rm *RoomManager) userExists(key string) bool {
	_, ok := rm.pointsMap[key]
	return ok
}

func (rm *RoomManager) deleteUser(key string) {
	rm.mu.Lock()
	delete(rm.pointsMap, key)
	delete(rm.hiddenMap, key)
	rm.mu.Unlock()
	rm.broker.updateChan <- true
}

func (rm *RoomManager) addUser(key string) {
	rm.mu.Lock()
	rm.pointsMap[key] = "0"
	rm.hiddenMap[key] = ""
	rm.mu.Unlock()
	rm.broker.updateChan <- true
}

func (rm *RoomManager) setUserPoints(key string, value string) {
	rm.mu.Lock()
	rm.pointsMap[key] = value
	rm.hiddenMap[key] = "?"
	rm.mu.Unlock()
	rm.broker.updateChan <- true
}

func setUserPointsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	roomUUID := req.URL.Query().Get("roomUUID")
	if roomUUID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		log.Print("Map does not contain room:", roomUUID)
		http.Error(w, "Internal Server Error", 500)
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

	username := data["username"].(string)
	points := data["points"].(string)

	// Don't want unconnected users to
	// add themselves
	if !roomManager.userExists(username) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomManager.setUserPoints(username, points)

	w.WriteHeader(http.StatusOK)
}

func resetAllPointsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	roomUUID := req.URL.Query().Get("roomUUID")
	if roomUUID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		log.Print("Map does not contain room:", roomUUID)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	verifyHash := req.Header.Get("X-Admin-Hash")
	if verifyHash != roomManager.adminHash {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	fmt.Println("Resetting all keys")
	roomManager.resetPoints()

	w.WriteHeader(http.StatusOK)
}

func togglePointsVisibilityHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	roomUUID := req.URL.Query().Get("roomUUID")
	if roomUUID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		log.Print("Map does not contain room:", roomUUID)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	verifyHash := req.Header.Get("X-Admin-Hash")
	if verifyHash != roomManager.adminHash {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	fmt.Println("Toggling key visibility")
	roomManager.togglePointsVisibility()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("true"))
}

type MainPageDetails struct {
	RoomUUID  string
	Key       string
	AdminHash string
}

// Serves the main page for both Admins and Users
func mainPageHandler(w http.ResponseWriter, req *http.Request) {
	roomUUID := strings.TrimPrefix(req.URL.Path, "/room/")
	fmt.Println("Got roomUUID: ", roomUUID)
	username := req.URL.Query().Get("username")

	if username == "" {
		redirectURL := fmt.Sprintf("login?roomUUID=%s", roomUUID)
		http.Redirect(w, req, redirectURL, http.StatusTemporaryRedirect)
	}

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		log.Print("Map does not contain room:", roomUUID)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	forbiddenTBAdmin := username == TBADMIN && roomManager.isTBAdminLoggedIn
	emptyKey := username == ""
	tooManyUsers := len(roomManager.pointsMap) >= MAX_USERS

	if forbiddenTBAdmin || emptyKey || roomManager.userExists(username) || tooManyUsers {
		http.Redirect(w, req, "/", http.StatusForbidden)
		return
	}

	var randString string
	if username == TBADMIN {
		randString = uuid.NewString()
		roomManager.adminHash = randString
		roomManager.isTBAdminLoggedIn = true
	} else {
		randString = ""
	}

	mainPageDetails := MainPageDetails{roomUUID, username, randString}

	fmt.Println("Serving main page for user:", mainPageDetails)
	if err := mainPage.Execute(w, mainPageDetails); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func landingPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving landing page for", req.Host)

	if err := landingPage.Execute(w, nil); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

type LoginPageDetails struct {
	RoomUUID string
}

func loginPageHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("Serving login page for", req.Host)

	roomUUID := req.URL.Query().Get("roomUUID")
	if err := loginPage.Execute(w, LoginPageDetails{roomUUID}); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func createRoomHandler(w http.ResponseWriter, req *http.Request) {
	// if req.Method != "POST" {
	// 	w.WriteHeader(http.StatusMethodNotAllowed)
	// 	return
	// }

	var roomUUID string = uuid.NewString()
	fmt.Println("Creating a room with uuid", roomUUID)

	roomBroker := &Broker{
		make(map[chan bool]bool),
		make(chan (chan bool)),
		make(chan (chan bool)),
		make(chan bool),
	}

	roomManager := &RoomManager{
		mu:                sync.Mutex{},
		pointsMap:         make(map[string]string),
		hiddenMap:         make(map[string]string),
		isMapVisible:      true,
		isTBAdminLoggedIn: false,
		adminHash:         "",
		broker:            roomBroker,
	}

	roomManager.broker.Start()

	roomMap[roomUUID] = roomManager
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(roomUUID))
	return
}

// Opens the connection with client
// and remains open until client closes connection
func sseEventHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Println("connecting to ", req.Host)

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Make sure that the writer supports flushing.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	roomUUID := req.URL.Query().Get("roomUUID")
	username := req.URL.Query().Get("username")
	fmt.Println(username, " subscribed to room", roomUUID)

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		log.Print("Map does not contain room:", roomUUID)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	roomBroker := roomManager.broker

	clientChan := make(chan bool)
	roomBroker.newClients <- clientChan

	if username == TBADMIN {
		roomManager.isTBAdminLoggedIn = true
		roomManager.setPointsVisibility(false)
	} else {
		roomManager.addUser(username)
	}

	payload := roomManager.getPointsPayload()
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()

	// Listen to the closing of the http connection via the CloseNotifier
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		fmt.Println(username, "has disconnected")

		if roomManager.isTBAdminLoggedIn && username == TBADMIN {
			roomManager.isTBAdminLoggedIn = false
			roomManager.setPointsVisibility(true)
		} else {
			roomManager.deleteUser(username)
		}
		roomBroker.dcClients <- clientChan
	}()

	// Don't close the connection, instead loop endlessly.
	for {
		// Notify the client via its routine
		// that there is an update
		_, open := <-clientChan

		if !open {
			fmt.Println("Channel for", username, "is closed")
			break
		}

		payload := roomManager.getPointsPayload()
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
}

func main() {
	var err error
	mainPage, err = template.ParseFS(files, "templates/main_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	landingPage, err = template.ParseFS(files, "templates/landing_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	loginPage, err = template.ParseFS(files, "templates/login_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", landingPageHandler)
	mux.HandleFunc("POST /setuserpoints", setUserPointsHandler)

	mux.HandleFunc("/togglepointsvisibility", togglePointsVisibilityHandler)
	mux.HandleFunc("/resetuserpoints", resetAllPointsHandler)

	mux.HandleFunc("GET /room/{roomUUID}", mainPageHandler)

	mux.HandleFunc("POST /room/{$}", createRoomHandler)

	mux.HandleFunc("GET /login/{$}", loginPageHandler)

	mux.HandleFunc("GET /sse_events/{$}", sseEventHandler)

	log.Fatal(http.ListenAndServe(":8090", mux))
}
