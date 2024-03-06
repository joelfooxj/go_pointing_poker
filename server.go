// Code modified from https://github.com/kljensen/golang-html5-sse-example
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
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
	errorPage   *template.Template

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

	// A channel to receive the teardown signal for the room
	teardownChan chan bool
}

func (b *Broker) Listen() {
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
				// Cloes the channel
				// Remove the client from the set/map
				close(c)
				delete(b.clients, c)
			case hasUpdate := <-b.updateChan:
				// Iterate through and relay the update signal to
				// each client channel
				// log.Printf("Notifying %d clients", len(b.clients))
				for c := range b.clients {
					c <- hasUpdate
				}
			case <-b.teardownChan:
				// log.Printf("Teardown signal received")
				// Close all client channels and end the routine
				for c := range b.clients {
					close(c)
				}
				b.clients = nil
				close(b.newClients)
				close(b.dcClients)
				close(b.updateChan)
				close(b.teardownChan)
				return
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

func roomTeardown(roomUUID string) {
	// Close all channels and end all go routines
	// nil the room

	// log.Print("Tearing down room ", roomUUID)
	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		errMsg := fmt.Sprintf("Room %s does not exist.", roomUUID)
		log.Print(errMsg)
		return
	}

	roomManager.broker.teardownChan <- true
	roomManager.pointsMap = nil
	roomManager.hiddenMap = nil
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
	roomUUID := req.PathValue("roomUUID")
	username := req.PathValue("username")
	points := req.PathValue("points")

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		http.NotFound(w, req)
		return
	}

	// Check that user exists
	if !roomManager.userExists(username) {
		errMsg := fmt.Sprintf("User %s already exists.", username)
		log.Print(errMsg)
		http.Error(w, errMsg, 403)
		return
	}

	roomManager.setUserPoints(username, points)
	w.WriteHeader(http.StatusOK)
}

func resetAllPointsHandler(w http.ResponseWriter, req *http.Request) {
	roomUUID := req.PathValue("roomUUID")
	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		http.NotFound(w, req)
		return
	}

	verifyHash := req.Header.Get("X-Admin-Hash")
	if verifyHash != roomManager.adminHash {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	roomManager.resetPoints()
	w.WriteHeader(http.StatusOK)
}

func togglePointsVisibilityHandler(w http.ResponseWriter, req *http.Request) {
	roomUUID := req.PathValue("roomUUID")
	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		http.NotFound(w, req)
		return
	}

	verifyHash := req.Header.Get("X-Admin-Hash")
	if verifyHash != roomManager.adminHash {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	roomManager.togglePointsVisibility()
	w.WriteHeader(http.StatusOK)
}

type MainPageDetails struct {
	RoomUUID  string
	Key       string
	AdminHash string
}

// Serves the main page for both Admins and Users
func mainPageHandler(w http.ResponseWriter, req *http.Request) {
	roomUUID := req.PathValue("roomUUID")
	username := req.URL.Query().Get("username")

	if username == "" {
		http.Redirect(w, req, "/", http.StatusTemporaryRedirect)
	}

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		errMsg := fmt.Sprintf("Room %s does not exist.", roomUUID)
		log.Print(errMsg)
		errorPageRedirectHandler(w, http.StatusNotFound, errMsg)
		return
	}

	if roomManager.userExists(username) {
		errorPageRedirectHandler(
			w,
			http.StatusForbidden,
			fmt.Sprintf("User %s already exists", username),
		)
		return
	}

	adminAlreadyPresent := username == TBADMIN && roomManager.isTBAdminLoggedIn
	tooManyUsers := len(roomManager.pointsMap) >= MAX_USERS

	if adminAlreadyPresent {
		errorPageRedirectHandler(
			w,
			http.StatusForbidden,
			fmt.Sprintf("Admin is already logged in."),
		)
		return
	}

	if tooManyUsers {
		errorPageRedirectHandler(
			w,
			http.StatusForbidden,
			fmt.Sprintf("This room has too many users."),
		)
		return
	}

	var randString string
	if username == TBADMIN {
		randString = roomManager.adminHash
		roomManager.isTBAdminLoggedIn = true
	} else {
		randString = ""
	}

	mainPageDetails := MainPageDetails{roomUUID, username, randString}

	if err := mainPage.Execute(w, mainPageDetails); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

func landingPageHandler(w http.ResponseWriter, req *http.Request) {
	if err := landingPage.Execute(w, nil); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", 500)
		return
	}
}

type LoginPageDetails struct {
	RoomUUID string
}

func createRoomHandler(w http.ResponseWriter, req *http.Request) {
	var roomUUID string = uuid.NewString()

	roomBroker := &Broker{
		make(map[chan bool]bool),
		make(chan (chan bool)),
		make(chan (chan bool)),
		make(chan bool),
		make(chan bool),
	}

	roomManager := &RoomManager{
		mu:                sync.Mutex{},
		pointsMap:         make(map[string]string),
		hiddenMap:         make(map[string]string),
		isMapVisible:      true,
		isTBAdminLoggedIn: false,
		adminHash:         uuid.NewString(),
		broker:            roomBroker,
	}

	roomManager.broker.Listen()

	roomMap[roomUUID] = roomManager
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(roomUUID))
	return
}

// Opens the connection with client
// and remains open until client closes connection
func sseEventHandler(w http.ResponseWriter, req *http.Request) {

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

	roomManager, keyExists := roomMap[roomUUID]
	if !keyExists {
		errMsg := fmt.Sprintf("Room %s does not exist.", roomUUID)
		log.Print(errMsg)
		http.Error(w, errMsg, 404)
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
	// or a teardown signal to end the routine.
	notify := w.(http.CloseNotifier).CloseNotify()
	teardownChan := make(chan bool)
	go func() {
		select {
		case <-notify:
			// client has left client-side
			// fmt.Println(username, "has disconnected")
			if roomManager.isTBAdminLoggedIn && username == TBADMIN {
				roomTeardown(roomUUID)
			} else {
				roomBroker.dcClients <- clientChan
				roomManager.deleteUser(username)
			}
			return
		case <-teardownChan:
			// Teardown has occured
			// End the routine
			// fmt.Println("Stopping notify routine for", username)
			return
		}
	}()

	// Don't close the connection, instead loop endlessly.
	for {
		// Notify the client via its routine
		// that there is an update
		_, open := <-clientChan

		if !open {
			// fmt.Println("Channel for", username, "is closed.")
			teardownChan <- true
			break
		}

		payload := roomManager.getPointsPayload()
		fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
}

type ErrorPageDetails struct {
	StatusCode   string
	ErrorMessage string
}

func errorPageRedirectHandler(w http.ResponseWriter, status int, msg string) {
	errorPageDetails := ErrorPageDetails{fmt.Sprintf("%d", status), msg}

	if err := errorPage.Execute(w, errorPageDetails); err != nil {
		log.Print(err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	return
}

func main() {
	log.Print("Starting TB Pointing Poker")

	var err error
	mainPage, err = template.ParseFS(files, "templates/main_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	landingPage, err = template.ParseFS(files, "templates/landing_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	errorPage, err = template.ParseFS(files, "templates/error_page.tmpl.html")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", landingPageHandler)
	mux.HandleFunc("POST /room/{roomUUID}/user/{username}/points/{points}", setUserPointsHandler)

	mux.HandleFunc("POST /room/{roomUUID}/visibility", togglePointsVisibilityHandler)
	mux.HandleFunc("POST /room/{roomUUID}/reset", resetAllPointsHandler)

	mux.HandleFunc("GET /room/{roomUUID}", mainPageHandler)

	mux.HandleFunc("POST /room/{$}", createRoomHandler)

	mux.HandleFunc("GET /sse_events/{$}", sseEventHandler)

	log.Fatal(http.ListenAndServe(":8090", mux))

	// log.Fatal(http.ListenAndServeTLS(
	// 	":443",
	// 	"/etc/letsencrypt/live/tbpointingpoker.com/fullchain.pem",
	// 	"/etc/letsencrypt/live/tbpointingpoker.com/privkey.pem",
	// 	mux))
}
