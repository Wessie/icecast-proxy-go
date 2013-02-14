package server

import (
    "bufio"
    "net"
)

type ClientID struct {
    /* 
    A struct that identifies a specific client and mount
    
    This exists because we need a way to link a random request
    to the metadata URL to an actual source connection. This
    type tries to collect as many as unique identifiers as possible
    and then bundles them for easiness.
    */
    // The mountpoint they are using.
    Mount string
    // User struct used by the authentication
    User *UserID
}

func NewClientID(mount string, user *UserID) *ClientID {
    return &ClientID{mount, user}
}

type Client struct {
    // identifier of the client
    ClientId *ClientID
    // Metadata send by this client (mp3 only)
    Metadata string
    // ReadWriter around the connection socket
    Bufrw *bufio.ReadWriter
    // The raw connection socket
    Conn net.Conn
}

func NewClient(conn net.Conn, bufrw *bufio.ReadWriter,
               id *UserID, mount string) *Client {
    
    clientId := NewClientID(mount, id)
    
    new := Client{clientId, "", bufrw, conn}
    return &new
}

type Mount struct {
    // A mapping from identifiers to known source clients
    Clients map[ClientID] *Client
    // The currently active client on the stream
    Active ClientID
    // The mount we are representing.
    Mount string
}

func NewMount(mount string) *Mount {
    clients := make(map[ClientID] *Client, 5)
    
    new := Mount{Clients: clients, Mount: mount}
    
    return &new
}

type Manager struct {
    /* A construct that contains the state used by the
    managing of the source client connections */
    Mounts []*Mount
    Receiver chan *Client
}

func NewManager() *Manager {
    mounts := make([]*Mount, 5)
    receiver := make(chan *Client, 5)
    
    return &Manager{mounts, receiver}
}

var ClientManager *Manager

func init() {
    ClientManager = NewManager()
}