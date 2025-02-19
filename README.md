# jf_pointing_poker
Simple pointing poker app served up in a single GO build.

# How to run
- Clone this repo 
- Enter the root folder and enter `go build server.go`. This should produce a binary file called `server`. Run this file.

# How to use
- In the browser, enter the browser and navigate to the address.

# Functionality
This is a webserver that allows users to create rooms, where other users can join with a unique and custom username. The user that first created the room is designated the `Admin` and is given admin functionality within the room, such as hiding points and resetting points. Non-admins (`peers`), will appear with their chosen usernames in a list. Peers are given the ability to choose points. These points will show up alongside their corresponding username. 

Pages are constructed via the tmpl.html files that are read in when the program first starts. Using the standard `http` library that Go provides, we assign handlers to routes/paths that we've specified. These handlers will then serve the pages that were read in previously. 

## Overview 
The following is an architectural overview of the app. 

### Data
All data is stored as an in-memory map that is created on starting the app. 
Each newly-created room is given a UUID that peers can use to join. This UUID serves as the key to a RoomManager object, which is what is used to manage the state of the room. 

Each RoomManager provides data structures to store points, users, and other state variables, as well as methods to set/get them. Note that updating and reading of the points map is gated with a mutex, to ensure that updates are atomic. 

```
{
  [room1UUID]: {...},
  [room2UUID]: {...},
  [room3UUID]: { RoomManager },
  ...,
  [roomN_UUID]: { 
    pointsMap: {
      [usernameA]: 2, 
      [usernameB]: 3, 
      ...,
    },
    ...,
    adminHash: 'someUUID',
    broker: RoomBroker
   },
}
```

### Broker 
An important client-server relationship pattern here is the Broker pattern. Every room is created with an instance of a Broker `struct`. It stores and handles the `chan`s of peers that have connected to the room. It is the mechanism that allows peers to subscribe to the room and get notified of updates. 

### SSE
Every admin and peer stays connected to the app via an SSE connection that is opened once the main page is loaded. The handler for each SSE connection will create the `chan` for that particular user and store it via the Broker mentioned above. This SSE handler will listen for disconnect notifications and pass this on to the Broker to remove the client from the room. The handler also listens to updates about the room and sends it to the associated client. 

