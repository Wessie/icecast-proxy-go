package server

import (
	"encoding/base64"
	"strings"

	"github.com/Wessie/icecast-proxy-go/config"
	"github.com/Wessie/icecast-proxy-go/http"
)

// For authentication access
import (
	"database/sql"

	"https://golang.org/x/crypto/bcrypt"
	_ "github.com/go-sql-driver/mysql"
)

var database *sql.DB
var receiveCredentials *sql.Stmt

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
	}
}

func FetchUser(user string) (hash string, perm Permission, err error) {
	user = strings.ToLower(user)

	tx, err := database.Begin()
	if err != nil {
		return "", PERM_NONE, LOGIN_ERR_REJECTED
	}
	defer tx.Commit()

	row := tx.Stmt(receiveCredentials).QueryRow(user)

	var tmpPerm int
	if err := row.Scan(&hash, &tmpPerm); err != nil {
		return "", PERM_NONE, LOGIN_ERR_REJECTED
	}

	return hash, NewPermission(tmpPerm), nil
}

func LoginClient(client *ClientID) (err error) {
	hash, perm, err := FetchUser(client.Name)
	if err != nil {
		// error fetching the user from the database
		return LOGIN_ERR_REJECTED
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(client.Pass)) == nil {
		// Don't forget to set the permission on the client object
		client.Perm = perm
		return nil
	}

	return LOGIN_ERR_REJECTED
}

/* The realm used and send to the user browser when trying to access
the HTTP pages. */
var realm = "R/a/dio"

func (c *ClientID) Login() (err error) {
	if c.Name == "source" {
		/* If the user is set to 'source' we need to make sure the
		   actual username isn't in the password field as a | separated
		   value */
		if strings.Contains(c.Pass, "|") {
			temp := strings.SplitN(c.Pass, "|", 2)
			c.Name, c.Pass = temp[0], temp[1]
		}
		/* We can be fairly sure that the login will fail if the name
		   is "source" but the password field does not contain any '|'.
		   We continue here nonetheless */
	}

	if !config.Authentication {
		// If the starter disabled auth we want to use ADMIN rights.
		c.Perm = PERM_ADMIN
		return nil
	}

	return LoginClient(c)
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

	if len(pair) != 2 {
		return
	}

	username, password = pair[0], pair[1]
	return
}

func AuthenticationError(w http.ResponseWriter, r *http.Request, err error) {
	/* Returns an authentication icecast error page when called. */
	w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
	w.WriteHeader(401)

	response := "401 Unauthorized\n"
	if err != nil {
		response += err.Error()
	}
	w.Write([]byte(response))
}

type AuthedHandler func(http.ResponseWriter, *http.Request, *ClientID)

func makeAuthHandler(fn AuthedHandler, perm Permission) http.HandlerFunc {
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
