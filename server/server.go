package server

import (
	"net/http"
)



func adminHandler(w http.ResponseWriter, r *http.Request, user *UserID) {
    /* Creates admin panel access to the proxy. 

    There are two URLs special cased that only require source level
    permission. These are the two required for source metadata (mp3 only)
    and mount information. Used by some source clients.
    */
    return
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
    /* The root handler of the server, this handler has to determine
    if something is a valid mount (and has to pass to the sourceHandler)
    or is a different valid or invalid URL. 
    */
    
    if r.Method == "SOURCE" {
        /* This is a new icecast source, pass it to the separate handler */
        makeAuthHandler(sourceHandler)(w, r)
    } else if r.Method == "GET" {
        path := r.URL.Path
        lenPath := len(path)
        if path == "/admin/metadata" {
            /* An mp3 metadata update */
            makeAuthHandler(metadataHandler)(w, r)
        } else if path == "/admin/listclients" {
            /* A request to get the mountpoint listeners. */
            return
        } else if lenPath >= 6 && path[:6] == "/admin" {
            /* Admin access, pass it to the handler */
            makeAuthHandler(adminHandler)(w, r)
        } else {
            http.NotFound(w, r)
        }
    } else {
        errorHandler(w, r, "Unsupported HTTP Method")
    }
}

func errorHandler(w http.ResponseWriter, r *http.Request, error string) {
    /* Returns a generic error */
    return
}

func metadataHandler(w http.ResponseWriter, r *http.Request,
                     user *UserID) {
    /* Handles a metadata request from a source. This should make sure
    an user cannot set the metadata of another users stream and even save
    metadata of inactive users */
    return
}

func sourceHandler(w http.ResponseWriter, r *http.Request, user *UserID) {
    /* Handler for icecast source requests. This can only be called by
    authenticated requests */
    hj, ok := w.(http.Hijacker)
    if !ok {
        http.Error(w, "Webserver doesn't support hijacking.",
                   http.StatusInternalServerError)
        return
    }
    
    conn, bufrw, err := hj.Hijack()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Create a client struct, this is defined in client.go
    client := NewClient(conn, bufrw, user, "")
    
    ClientManager.Receiver <- client
    
    // The manager will handle everything from this point on
    return
}

func Initialize() {
	http.HandleFunc("/admin", makeAuthHandler(adminHandler))
	http.HandleFunc("/", mainHandler)

	http.ListenAndServe(":8050", nil)
}