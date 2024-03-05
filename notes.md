I wonder if we can use an in-memory kv-store.

Eg. have room_uuid + peer_uuid? 

We can guarantee that the room_uuid + peer_uuid is unique. 

If we update the in-memory kv-store, we should only be updating the particular key - a single part of the memory.

Right now,
- every new peer needs to get its own SSE channel - are channels expensive? 
- ecery time an update happens to the points map, a SSE event is sent to all peers
  - the question then is: how do we send events to only the peers we want? 

Why not just have a map of maps? 
Do we need a dedicated broker for each room? 
1 broker should be enough no? 
```js
{
  room_uuid: {
    points_map: {
      peerA: value
    }, 
    peer_chan: chan[]
  }
}
```
1. 
2. peer A in room 123 sends a value
   1. We don't need to be concerned about peer A sending values to another room since they would have to know the room_uuid of the other room 
3. Grab the points_map for the room, update the points_map
4. Send the updated map to all peer chans for the this room

# host flow 
1. host enters 

We need a landing page - otherwise new users are confused
A user should also be able to directly join a room with a correct URL

# Flows to enter the main page
1. landing page -> login page -> main page 
2. copied roomURL -> login page -> main page 
3. full URL -> main page

Each room needs to maintain its own set of channels, so any changes to that room are propogated only its set of channels 
Question: does each room require its own broker then? Seems like it