package server

import (
    "bufio"
    "io"
    "net"
    "hash/fnv"
    "strings"
    "github.com/Wessie/icecast-proxy-go/http"
    "github.com/Wessie/icecast-proxy-go/shout"
    "github.com/Wessie/icecast-proxy-go/config"
)

/* LoginStatus is an error returned when anything goes wrong in the
process of retrieving and verifying login credentials */
type LoginStatus int

const (
    LOGIN_ERR_REJECTED LoginStatus = 1
    LOGIN_ERR_EMPTY = 2
)

// We use a simple map to support human readable error strings.
var loginErrorStrings = map[LoginStatus] string {
    LOGIN_ERR_REJECTED: "Invalid credentials",
    LOGIN_ERR_EMPTY: "Empty credentials",
}

func (self LoginStatus) Error () string {
    return loginErrorStrings[self]
}


/* 
Signifies a permission level in the authentication system. 

The enum below sets the various possible levels.
*/
type Permission int8

/* The different kind of permissions used in the proxy */
const (
    PERM_NONE Permission = iota // Unable to do anything
    PERM_META // Able to edit current active metadata (mp3 only)
    PERM_SOURCE // Able to be a source on the server
    PERM_ADMIN // Admin access, can do anything
)

/* 
A struct that identifies a specific client and mount

This exists because we need a way to link a random request
to the metadata URL to an actual source connection. This
type tries to collect as many as unique identifiers as possible
and then bundles them for easiness.
*/
type ClientID struct {
    // Name given by the client, might be empty.
    Name string
    // Password given by the client, might be empty
    Pass string
    // The permission level of this client.
    Perm Permission
    // The useragent used by the client
    Agent string
    // Address of the client.
    Addr string
    // Mountpoint requested, "" if not used
    Mount string
    // Audio data format, "" if not used
    AudioFormat string
}

func NewClientIDFromRequest(r *http.Request) (client *ClientID) {
    client = &ClientID{}
    
    switch cont := r.Header.Get("Content-Type"); {
        case cont == "audio/mpeg":
            client.AudioFormat = "MP3"
        case cont == "audio/ogg", cont == "application/ogg":
            client.AudioFormat = "OGG"
        default:
            client.AudioFormat = ""
    }
    
    if path := r.URL.Path; path == "/admin/metadata" || path == "/admin/listclients" {
        parsed := r.URL.Query()
        client.Mount = parsed.Get("mount")
    } else {
        client.Mount = path
    }
    // The user should have no permissions on creation.
    client.Perm = PERM_NONE
    
    // Retrieve credentials from the request (Basic Authorization)
    // These are empty strings if no auth was found.
    client.Name, client.Pass = ParseDigest(r)
    
    // The address used by the client.
    client.Addr = r.RemoteAddr    
    
    // Retrieve the useragent from the request
    client.Agent = r.Header.Get("User-Agent")
    
    return
}

func (self *ClientID) Hash() (ClientHash) {
    h := fnv.New64a()
    // Okey lets start hashing this slowly
    io.WriteString(h, self.Name)
    io.WriteString(h, self.Pass)
    io.WriteString(h, self.Mount)
    // The address also contains the port... get rid of it!
    s := strings.Split(self.Addr, ":")
    io.WriteString(h, s[0])
    
    return ClientHash(h.Sum64())
}

type ClientHash uint64

type Client struct {
    // identifier of the client
    ClientID *ClientID
    // Metadata send by this client (mp3 only)
    Metadata string
    // ReadWriter around the connection socket
    Bufrw *bufio.ReadWriter
    // The raw connection socket
    Conn net.Conn
}

func NewClient(conn net.Conn, bufrw *bufio.ReadWriter,
               clientID *ClientID) *Client {
    
    new := Client{clientID, "", bufrw, conn}
    return &new
}

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

func DestroyMount(mount *Mount) {
    shout.DestroyShout(*mount.Shout)
}

type Manager struct {
    /* A construct that contains the state used by the
    managing of the source client connections */
    Mounts map[string] *Mount
    // A channel to receive new clients from
    Receiver chan *Client
    // A channel that allows to register mounts as empty
    // this way we can clean them up outside client logic.
    MountCollector chan *Mount
    // A channel to receive metadata on
    MetaChan chan *MetaPack
    // This is a mapping to store a temporary metadata copy
    metaStore map[ClientHash]string
}

func NewManager() *Manager {
    mounts := make(map[string] *Mount, 5)
    receiver := make(chan *Client, 5)
    collector := make(chan *Mount, 5)
    meta := make(chan *MetaPack, 10)
    metastore := make(map[ClientHash]string, 5)
    
    return &Manager{Mounts: mounts,
                    Receiver: receiver,
                    MountCollector: collector,
                    MetaChan: meta,
                    metaStore: metastore}
}

type MetaPack struct {
    /* A simple struct to pass through the Meta Channel */
    Data string
    ID *ClientID
}

type DataPack struct {
    /* A simple struct to contain both data and the client identifier
    
    There is no way to identify the data if we don't use this
    */
    Data []byte
    Client *Client
}

type ErrPack struct {
    /* Just as the DataPack this is a simple struct containing the
    client identifier together with the error */
    Err error
    Client *Client
}

type FullQueue struct {}

func (self *FullQueue) Error() string {
    return "Client queue exceeded, discarding client."
}