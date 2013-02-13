package server

import (
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
    database, err = sql.Open("mysql", "user:password@/dbname?charset=utf8")
    if err != nil {
        panic(err)
    }
    receiveCredentials, err = database.Prepare("SELECT user FROM users WHERE user=? LIMIT 1;")
    if err != nil {
        panic(err)
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
type Perm int8

/* The different kind of permissions used in the proxy */
const (
	PERM_ADMIN Perm = iota // Admin access, can do anything
	PERM_META // Able to edit current active metadata (mp3 only)
	PERM_SOURCE // Able to be a source on the server
)

type User struct {
	name string
	perm Perm
}


func Login(username string, password string) (user User, err error) {
    /* Logs in an user */
    if username == "source" {
        /* If the user is set to 'source' we need to make sure the
        actual username isn't in the password field as a | separated
        value */
        if strings.Contains(password, "|") {
            temp := strings.SplitN(password, "|", 2)
            username, password = temp[0], temp[1]
        }
        /* We can be fairly sure that the login will fail if the name
        is "source" but the password field does not contain any '|'.
        We continue here nonetheless */
    }
    // All the code above should not be touched unless you know what
    // you are doing to begin with.
    
    perm := PERM_ADMIN
    /* Continue like normal here */

    transaction, err := database.Begin()
    if err != nil {
        log.Fatal(err)
    }
    
    row := transaction.Stmt(receiveCredentials).QueryRow(username)
    
    var hash string
    err = row.Scan(&hash)
    if err == sql.ErrNoRows {
        return User{}, LOGIN_ERR_REJECTED
    } else if err != nil {
        // Unexpected error happened?
        log.Fatal(err)
    }
    
    /* We are in the clear, lets check out if we have the correct password */
    if bcrypt.Match(password, hash) {
        return User{username, perm}, nil
    }
    
    return User{}, LOGIN_ERR_REJECTED
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
                             user User),) http.HandlerFunc {
	/* Makes a handler closure that returns an error page
	   when the requested page requires authentication and no
	   authentication or appropriate permissions are set */

    wrapped := func(w http.ResponseWriter, r *http.Request) {
            // Get the login credentials from the request
            username, password := ParseDigest(r)
            if username == "" && password == "" {
                AuthenticationError(w, r, nil)
                return
            }
            
            // Check the login credentials
            user, err := Login(username, password)
            if err != nil {
                AuthenticationError(w, r, err)
                return
            }
            fn(w, r, user)
        }
	return wrapped
}

