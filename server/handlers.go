package server

import (
	"sync"
)

var HandlerMounts map[string][]*Client = map[string][]*Client{}

var HandlerLock sync.Mutex = sync.Mutex{}

/*
Called whenever a new client connects.
*/
func HandleClientConnect(client *Client) {
	var Clients []*Client
	HandlerLock.Lock()
	if c, ok := HandlerMounts[client.ClientID.Mount]; ok {
		Clients = c
	} else {
		Clients = make([]*Client, 0)
	}
	Clients = append(Clients, client)
	HandlerMounts[client.ClientID.Mount] = Clients
	HandlerLock.Unlock()
}

/*
Called whenever a client disconnects. Actions on the clients network
members has undefined behaviour at this point.
*/
func HandleClientDisconnect(client *Client) {
	HandlerLock.Lock()
	Clients, ok := HandlerMounts[client.ClientID.Mount]
	if !ok {
		return
	}
	for i, c := range Clients {
		if c == client {
			Clients = append(Clients[:i], Clients[i+1:]...)
			HandlerMounts[client.ClientID.Mount] = Clients
			break
		}
	}
	if len(Clients) == 0 {
		delete(HandlerMounts, client.ClientID.Mount)
	}
	HandlerLock.Unlock()
}

/*
Called whenever a client is turned into 'live' mode.

The 'live' mode means that actual data is being send to the
icecast server for this client. When a client isn't in the 'live' mode
all data received is discarded.
*/
func HandleClientLive(client *Client) {
	Clients, ok := HandlerMounts[client.ClientID.Mount]
	if !ok {
		return
	}
	for i, c := range Clients {
		if c == client {
			Clients = append([]*Client{c}, append(Clients[:i], Clients[i+1:]...)...)
			HandlerMounts[client.ClientID.Mount] = Clients
			break
		}
	}
}

/*
Called whenever a client is removed from 'live' mode. See `HandleClientLive`
for a short description of the 'live' mode.
*/
func HandleClientUnlive(client *Client) {

}

/*
Called whenever new metadata is received for a client.

Repeated sends of the same metadata won't call this handler multiple
times. The same applies to rejected metadata, this won't call this
handler if the metadata is not accepted.
*/
func HandleMetadata(client *Client, metadata string) {

}
