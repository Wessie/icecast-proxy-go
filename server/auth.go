package server

import (
    "github.com/Wessie/icecast-proxy-go/config"
    "github.com/Wessie/icecast-proxy-go/http"
    "strings"
    "encoding/base64"
    "strconv"
    "bufio"
    "time"
    "log"
    "os"
)

// For authentication access
import (
    "code.google.com/p/go.crypto/bcrypt"
)

var CACHE_TTL time.Duration = time.Minute * 5
var authentication_filename = "auth.conf"
var user_cache *UserCache = NewUserCache()

/* userInfo contains relevant information we want to cache */
type userInfo struct {
    Pwd string
    Perm Permission
    last time.Time
}

func newUserInfo(pwd string, perm Permission) *userInfo {
    return &userInfo{
        Pwd: pwd,
        Perm: perm,
        last: time.Now(),
    }
}

func Init_auth() {
    user_cache = NewUserCache()

    if err := user_cache.LoadAll(); err != nil {
        panic(err)
    }
}

/* UserCache embeds a map type with handy methods to fetch items */
type UserCache struct {
    cache map[string] *userInfo
}

func (self *UserCache) LoadAll() (err error) {
    file, err := os.Open(authentication_filename)
    if err != nil {
        log.Fatal("Could not load authentication file.")
    }
    
    scanner := bufio.NewScanner(file)
    scanner.Split(bufio.ScanWords)
    
    var user, hash string
    var priv Permission
    
    for scans := 0; scanner.Scan(); scans++ {
        switch scans {
        case 0:
            user = scanner.Text()
        case 1:
            hash = scanner.Text()
        case 2:
            sp := scanner.Text()
            p, err := strconv.Atoi(sp)
            if err != nil {
                p = 2
            }
            
            priv = NewPermission(p)
        }
        
        if scans >= 2 {
            self.cache[user] = newUserInfo(hash, priv)
            
            scans = 0
        }
    }
    
    return nil
}

/* Function that returns a copy from the cache or tries retrieving it from
the database if not in the cache. This does no actual authentication checking
*/
func (self *UserCache) Fetch(user string) (hash string, perm Permission, err error) {
    user = strings.ToLower(user)
    if value, ok := self.cache[user]; ok {
        return value.Pwd, value.Perm, nil
    }
    
    // It's not in the cache.. do an update
    return self.FetchUpdate(user)
}

func (self *UserCache) FetchUpdate(user string) (hash string, perm Permission, err error) {
    user = strings.ToLower(user)
    // Check our cache timeout
    if value, ok := self.cache[user]; ok {
        if time.Now().Sub(value.last) < CACHE_TTL {
            return value.Pwd, value.Perm, nil
        }
    } else {
        err = LOGIN_ERR_REJECTED
    }
    return
}

func (self *UserCache) Login(client *ClientID) (err error) {
    hash, perm, err := self.Fetch(client.Name)
    if err != nil {
        // We got an error back from the fetch... lets just assume a rejection
        return LOGIN_ERR_REJECTED
    }
    
    if bcrypt.CompareHashAndPassword([]byte(hash), []byte(client.Pass)) == nil {
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
    
    if bcrypt.CompareHashAndPassword([]byte(hash), []byte(client.Pass)) == nil {
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

