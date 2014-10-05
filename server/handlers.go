package server

/*
Called whenever a new client connects.
*/
func HandleClientConnect(client *Client) {

}

/*
Called whenever a client disconnects. Actions on the clients network
members has undefined behaviour at this point.
*/
func HandleClientDisconnect(client *Client) {

}

/*
Called whenever a client is turned into 'live' mode.

The 'live' mode means that actual data is being send to the
icecast server for this client. When a client isn't in the 'live' mode
all data received is discarded.
*/
func HandleClientLive(client *Client) {

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
