package server

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"strings"
	"sync/atomic"

	"github.com/Wessie/icecast-proxy-go/http"
)

/* LoginStatus is an error returned when anything goes wrong in the
process of retrieving and verifying login credentials */
type LoginStatus int

const (
	LOGIN_ERR_REJECTED LoginStatus = 1
	LOGIN_ERR_EMPTY                = 2
)

// We use a simple map to support human readable error strings.
var loginErrorStrings = map[LoginStatus]string{
	LOGIN_ERR_REJECTED: "Invalid credentials",
	LOGIN_ERR_EMPTY:    "Empty credentials",
}

func (self LoginStatus) Error() string {
	return loginErrorStrings[self]
}

/*
Signifies a permission level in the authentication system.

The enum below sets the various possible levels.
*/
type Permission int8

/* The different kind of permissions used in the proxy */
const (
	PERM_NONE   Permission = iota // Unable to do anything
	PERM_META                     // Able to edit current active metadata (mp3 only)
	PERM_SOURCE                   // Able to be a source on the server
	PERM_ADMIN                    // Admin access, can do anything
)

func NewPermission(perm int) Permission {
	switch perm {
	case 5:
		fallthrough
	case 4:
		return PERM_ADMIN
	case 3:
		fallthrough
	case 2:
		return PERM_SOURCE
	default:
		return PERM_NONE
	}
}

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

func (self *ClientID) Hash() ClientHash {
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

/* Returns a pretty string that contains information about the client.
Especially handy in logging and debugging */
func (self *Client) String() string {
	return fmt.Sprintf("[%p] %s@%s",
		self, self.ClientID.Name, self.ClientID.Addr)
}

func NewClient(conn net.Conn, bufrw *bufio.ReadWriter,
	clientID *ClientID) *Client {

	new := Client{clientID, "", bufrw, conn}
	return &new
}

/*
Container that makes it easier to handle fuzzy and exact matching
when it comes to clients. Since we don't want to clutter logic code
with this we do it all in a special type
*/
type ClientContainer struct {
	// A map that has the ClientHash as key.
	byHash map[ClientHash]*Client
	// A map that has the ClientID pointer as key.
	byID map[*ClientID]*Client
	// The current amount of clients in the container
	Length int32
}

/*
Gets a client from the container by the hash key

*/
func (self *ClientContainer) GetByHash(hash ClientHash) (client *Client, ok bool) {
	for id, client := range self.byID {
		if id.Hash() == hash {
			return client, true
		}
	}
	return nil, false
}

/*
Gets a client from the container by the ClientID pointer key

*/
func (self *ClientContainer) GetByID(id *ClientID) (client *Client, ok bool) {
	client, ok = self.byID[id]
	return client, ok
}

/*
Adds a client to the container.

This method is not thread-safe.
*/
func (self *ClientContainer) Add(client *Client) {
	self.byID[client.ClientID] = client
	atomic.AddInt32(&self.Length, 1)
}

/*
Removes a client from the container.

This method is not thread-safe.
*/
func (self *ClientContainer) Remove(client *Client) {
	delete(self.byID, client.ClientID)
	atomic.AddInt32(&self.Length, -1)
}

/*
Creates and returns a new ClientContainer

*/
func NewClientContainer() *ClientContainer {
	container := ClientContainer{}
	container.byID = make(map[*ClientID]*Client, 5)
	container.Length = 0
	return &container
}

/*
Closes all connections and then returns

*/
func (self *ClientContainer) Destroy() {
	// Yes we don't remove anything from the actual internal mapping.
	// Why? Because we assume the whole structure is going to be garbage
	// collected very soon since we are wished death.
	for _, client := range self.byID {
		client.Conn.Close()
	}
}
