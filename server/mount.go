package server

import (
    "github.com/Wessie/icecast-proxy-go/shout"
    "github.com/Wessie/icecast-proxy-go/config"
)

type Mount struct {
    // The queue of clients
    ClientQueue chan *ClientID
    // A mapping from identifiers to known source clients
    Clients map[ClientHash] *Client
    // The currently active client on the stream
    Active *ClientID
    // The mount we are representing.
    Mount string
    // The libshout instance we are using for this mount.
    Shout *shout.Shout
}

func NewMount(mount string) *Mount {
    clients := make(map[ClientHash] *Client, 5)
    
    queue := make(chan *ClientID, config.QUEUE_LIMIT)
    
    // Create a new libshout instance for us
    sh := shout.NewShout(config.CreateShoutMap())
    
    new := Mount{Clients: clients, Mount: mount, Shout: sh,
                 ClientQueue: queue}
    
    return &new
}

func DestroyMount(self *Mount) {
    shout.DestroyShout(*self.Shout)
    
    for _, client := range self.Clients {
        client.Conn.Close()
    }
}

type FullQueue struct {}

func (self *FullQueue) Error() string {
    return "Client queue exceeded, discarding client."
}