package server

import (
    "github.com/Wessie/icecast-proxy-go/config"
    "github.com/Wessie/icecast-proxy-go/http"
    "strings"
    "encoding/base64"
)

// For authentication access
import (
    "github.com/jameskeane/bcrypt"
    "database/sql"
    _ "github.com/Go-SQL-Driver/MySQL"

)

var database *sql.DB
var receiveCredentials *sql.Stmt
var getAllCredentials *sql.Stmt

/*
We don't want to continue the executation at all if the database
connection is down or broken. Thus we panic to let the user notice
this very quickly
*/
func Init_auth() {
    var err error
    if config.Authentication {
        database, err = sql.Open("mysql", config.CreateDatabaseDSN())
        if err != nil {
            panic(err)
        }
        receiveCredentials, err = database.Prepare("SELECT pass, privileges FROM users WHERE LOWER(user)=LOWER(?) LIMIT 1;")
        if err != nil {
            panic(err)
        }
        getAllCredentials, err = database.Prepare("SELECT LOWER(user), pass, privileges FROM users;")
        if err != nil {
            panic(err)
        }
    }
}

/* userInfo contains relevant information we want to cache */
type userInfo struct {
    Pwd string
    Perm Permission
}

func newUserInfo(pwd string, perm Permission) *userInfo {
    return &userInfo{pwd, perm}
}

/* UserCache embeds a map type with handy methods to fetch items */
type UserCache struct {
    cache map[string] *userInfo
}

func (self *UserCache) LoadAll() (err error) {
    // Start a database transaction
    transaction, err := database.Begin()
    if err != nil {
        // A database error.. most likely this is a DB down error.
        // Log the case and reject the login
        // TODO: Logging
        return LOGIN_ERR_REJECTED
    }
    
    // Use a prepared statement with the transaction.
    rows, err := transaction.Stmt(getAllCredentials).Query()
    
    if err != nil {
        return err
    }
    
    for rows.Next() {
        var user string
        var hash string
        var temp_perm int
        err = rows.Scan(&user, &hash, &temp_perm)
        
        perm := NewPermission(temp_perm)
        self.cache[user] = newUserInfo(hash, perm)
    }
    
    return nil
}

/* Function that returns a copy from the cache or tries retrieving it from
the database if not in the cache. This does no actual authentication checking
*/
func (self *UserCache) Fetch(user string) (hash string, perm Permission, err error) {
    if value, ok := self.cache[user]; ok {
        return value.Pwd, value.Perm, nil
    }
    
    // It's not in the cache.. do an update
    return self.FetchUpdate(user)
}

func (self *UserCache) FetchUpdate(user string) (hash string, perm Permission, err error) {
    // Start a database transaction
    transaction, err := database.Begin()
    if err != nil {
        // A database error.. most likely this is a DB down error.
        // Log the case and reject the login
        // TODO: Logging
        err = LOGIN_ERR_REJECTED
        return
    }
    
    // Use a prepared statement with the transaction.
    row := transaction.Stmt(receiveCredentials).QueryRow(user)
    
    var temp_perm int
    err = row.Scan(&hash, &temp_perm)
    
    if err == sql.ErrNoRows {
        err = LOGIN_ERR_REJECTED
        return
    } else if err != nil {
        // Unexpected error happened?
        // TODO: Logging
        err = LOGIN_ERR_REJECTED
        return
    }
    
    perm = NewPermission(temp_perm)

    self.cache[user] = newUserInfo(hash, perm)
    
    return
}

func (self *UserCache) Login(client *ClientID) (err error) {
    hash, perm, err := self.Fetch(client.Name)
    if err != nil {
        // We got an error back from the fetch... lets just assume a rejection
        return LOGIN_ERR_REJECTED
    }
    
    if bcrypt.Match(client.Pass, hash) {
        // Don't forget to set the permission on the client object
        client.Perm = perm
        return nil
    }
    
    // Ok well.. that is awkward.. either the cached version was wrong or...
    // the actual password differs totally. Lets try updating our cache first
    hash, perm, err = self.FetchUpdate(client.Name)
    if err != nil {
        // Same as above, assume a rejection reason
        return LOGIN_ERR_REJECTED
    }
    
    if bcrypt.Match(client.Pass, hash) {
        // Don't forget to.. set the permission
        client.Perm = perm
        return nil
    }
    
    // Well.. that means the password is just plain wrong! REJECTED
    return LOGIN_ERR_REJECTED
}

/* Returns a new initialized UserCache, this does not pre-load the cache.
Call UserCache.LoadAll() for loading everything into the cache */
func NewUserCache() *UserCache {
    cache := &UserCache{}
    cache.cache = make(map[string] *userInfo, 30)
    return cache
}

/* The realm used and send to the user browser when trying to access
the HTTP pages. */
var realm = "R/a/dio"
/* A cache that keeps passwords and usernames in a mapping, this reduces
database hits */
var user_cache = NewUserCache()

func (self *ClientID) Login() (err error) {
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
    
    return user_cache.Login(self)
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
                             user *ClientID), perm Permission) http.HandlerFunc {
    /* Makes a handler closure that returns an error page
       when the requested page requires authentication and no
       authentication or appropriate permissions are set */

    wrapped := func(w http.ResponseWriter, r *http.Request) {
            // Create a user object from the request
            user := NewClientIDFromRequest(r)
            
            if user.Pass == "" && user.Name == "" {
                AuthenticationError(w, r, nil)
                return
            }
            // Check the login credentials
            if err := user.Login(); err != nil {
                AuthenticationError(w, r, err)
                return
            }
            
            if user.Perm < perm {
                AuthenticationError(w, r, nil)
                return
            }
            
            fn(w, r, user)
        }
    return wrapped
}

