package server

import (
    "github.com/Wessie/icecast-proxy-go/config"
    "strings"
    "encoding/base64"
    "net/http"
    "log"
)

// For authentication access
import (
    "github.com/jameskeane/bcrypt"
    "database/sql"
    _ "github.com/Go-SQL-Driver/MySQL"

)

var database *sql.DB
var receiveCredentials *sql.Stmt

/*
We don't want to continue the executation at all if the database
connection is down or broken. Thus we panic to let the user notice
this very quickly
*/
func init() {
    var err error
    if config.Authentication {
        database, err = sql.Open("mysql", "user:password@/dbname?charset=utf8")
        if err != nil {
            panic(err)
        }
        receiveCredentials, err = database.Prepare("SELECT user FROM users WHERE user=? LIMIT 1;")
        if err != nil {
            panic(err)
        }
    }
}


/* The realm used and send to the user browser when trying to access
the HTTP pages. */
var realm = "R/a/dio"


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


/* A type for the permissions used in the proxy */
type Permission int8

/* The different kind of permissions used in the proxy */
const (
    PERM_NONE Permission = iota // Unable to do anything
    PERM_ADMIN // Admin access, can do anything
    PERM_META // Able to edit current active metadata (mp3 only)
    PERM_SOURCE // Able to be a source on the server
)

type UserID struct {
	Name string
    Pass string
	Perm Permission
    // The useragent used by the client
    Agent string
    Addr string
}

func NewUserFromRequest(r *http.Request) (user *UserID) {
    user = &UserID{}
    
    // The user should have no permissions on creation.
    user.Perm = PERM_NONE
    
    // Retrieve credentials from the request (Basic Authorization)
    // These are empty strings if no auth was found.
    user.Name, user.Pass = ParseDigest(r)
    
    // The address used by the client.
    user.Addr = r.RemoteAddr    
    
    // Retrieve the useragent from the request
    if useragent := r.Header.Get("User-Agent"); useragent != "" {
        user.Agent = useragent
    }
    
    return
}

func (self *UserID) Login() (err error) {
    /* Logs in an user */
    if self.Name == "source" {
        /* If the user is set to 'source' we need to make sure the
        actual username isn't in the password field as a | separated
        value */
        if strings.Contains(self.Pass, "|") {
            temp := strings.SplitN(self.Pass, "|", 2)
            self.Name, self.Pass = temp[0], temp[1]
        }
        /* We can be fairly sure that the login will fail if the name
        is "source" but the password field does not contain any '|'.
        We continue here nonetheless */
    }
    // All the code above should not be touched unless you know what
    // you are doing to begin with.
    
    if !config.Authentication {
        // If the starter disabled auth we want to use ADMIN rights.
        self.Perm = PERM_ADMIN
        return nil
    }
    
    /* Continue like normal here */

    transaction, err := database.Begin()
    if err != nil {
        log.Fatal(err)
    }
    
    row := transaction.Stmt(receiveCredentials).QueryRow(self.Name)
    
    var hash string
    err = row.Scan(&hash)
    
    if err == sql.ErrNoRows {
        return LOGIN_ERR_REJECTED
    } else if err != nil {
        // Unexpected error happened?
        log.Fatal(err)
    }
    
    /* We are in the clear, lets check out if we have the correct password */
    if bcrypt.Match(self.Pass, hash) {
        self.Perm = PERM_ADMIN
        return nil
    }
    
    return LOGIN_ERR_REJECTED
}

func ParseDigest(r *http.Request) (username string, password string) {
    authorization := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
    
    if len(authorization) != 2 || authorization[0] != "Basic" {
        return
    }
    
    decoded, err := base64.StdEncoding.DecodeString(authorization[1])
    
    if err != nil {
        return
    }
    
    pair := strings.SplitN(string(decoded), ":", 2)
    
    username, password = pair[0], pair[1]
    return
}

func AuthenticationError(w http.ResponseWriter, r *http.Request, err error) {
	/* Returns an authentication icecast error page when called. */
	w.Header().Set("WWW-Authenticate", `Basic realm="` + realm + `"`)
    w.WriteHeader(401)
    
    response := "401 Unauthorized\n"
    if err != nil {
        response += err.Error()
    }
    w.Write([]byte(response))
}

func makeAuthHandler(fn func(w http.ResponseWriter,
                             r *http.Request,
                             user *UserID),) http.HandlerFunc {
	/* Makes a handler closure that returns an error page
	   when the requested page requires authentication and no
	   authentication or appropriate permissions are set */

    wrapped := func(w http.ResponseWriter, r *http.Request) {
            // Create a user object from the request
            user := NewUserFromRequest(r)
            
            if user.Pass == "" && user.Name == "" {
                AuthenticationError(w, r, nil)
                return
            }
            // Check the login credentials
            if err := user.Login(); err != nil {
                AuthenticationError(w, r, err)
                return
            }
            fn(w, r, user)
        }
	return wrapped
}

