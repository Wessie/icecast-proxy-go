package server

import (
    "github.com/Wessie/icecast-proxy-go/http"
    "github.com/Wessie/icecast-proxy-go/config"
    "time"
    "fmt"
    "io"
    "strconv"
)



func adminHandler(w http.ResponseWriter, r *http.Request, clientID *ClientID) {
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
    fmt.Println(r.URL)
    if r.Method == "SOURCE" {
        /* This is a new icecast source, pass it to the separate handler */
        makeAuthHandler(sourceHandler, PERM_SOURCE)(w, r)
    } else if r.Method == "GET" {
        path := r.URL.Path
        lenPath := len(path)
        if path == "/admin/metadata" {
            /* An mp3 metadata update */
            makeAuthHandler(metadataHandler, PERM_META)(w, r)
        } else if path == "/admin/listclients" {
            /* A request to get the mountpoint listeners. */
            makeAuthHandler(listclientsHandler, PERM_SOURCE)(w, r)
        } else if lenPath >= 6 && path[:6] == "/admin" {
            /* Admin access, pass it to the handler */
            makeAuthHandler(adminHandler, PERM_ADMIN)(w, r)
        } else {
            http.NotFound(w, r)
        }
    } else {
        errorHandler(w, r, "Unsupported HTTP Method")
    }
}

func errorHandler(w http.ResponseWriter, r *http.Request, err string) {
    /* Returns a generic error */
    return
}

func listclientsHandler(w http.ResponseWriter, r *http.Request,
                     clientID *ClientID) {

    response := []byte("<?xml version=\"1.0\"?>\n<icestats><source mount=\"" + clientID.Mount + "\"><Listeners>0</Listeners></source></icestats>\n")

    w.Header().Set("Content-Length", strconv.Itoa(len(response)))
    w.Write(response)
}

func metadataHandler(w http.ResponseWriter, r *http.Request,
                     clientID *ClientID) {
    /* Handles a metadata request from a source. This should make sure
    an user cannot set the metadata of another users stream and even save
    metadata of inactive users */
    var meta string
    
    parsed := r.URL.Query()
    
    meta = parsed.Get("song")
    if meta == "" {
        // Someone is trying to not update the metadata?
        // Ignore the fucker
        return
    }
    
    var charset string
    if e := parsed.Get("charset"); e != "" {
        // TODO: Check if user specified encodings are not vulnerable to exploits
        charset = e
    } else {
        charset = "latin1"
    }

    // This isn't so much a parser as it is a encoding handler.
    meta = ParseMetadata(charset, meta)

    fmt.Println(meta)
    // Sending empty metadata is useless, so we don't
    if meta != "" {
        // And we are done here, send the data we have so far along
        ClientManager.MetaChan <- &MetaPack{Data: meta, ID: clientID}
    }
    
    response := []byte("<?xml version=\"1.0\"?>\n<iceresponse><message>Metadata update successful</message><return>1</return></iceresponse>\n")
    
    w.Header().Set("Content-Length", strconv.Itoa(len(response)))
    w.Write(response)
}

func sourceHandler(w http.ResponseWriter, r *http.Request, clientID *ClientID) {
    /* Handler for icecast source requests. This can only be called by
    authenticated requests */
    fmt.Println("New source request")
    // Icecast clients expect a 200 OK response before sending data.
    w.WriteHeader(http.StatusOK)
    // Make sure to send the extra newline to signify end of headers
    io.WriteString(w, "\r\n")
    // And flush the data because buffering is FUN!
    if flush, ok := w.(http.Flusher); ok {
        flush.Flush()
    }    
    
    // We can now start hijacking the connection
    hj, ok := w.(http.Hijacker)
    if !ok {
        http.Error(w, "Webserver doesn't support hijacking.",
                   http.StatusInternalServerError)
        fmt.Println("Webserver doesn't support hijacking.")
        return
    }
    
    conn, bufrw, err := hj.Hijack()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        fmt.Println(err.Error())
        return
    }
    
    // Create a client struct, this is defined in client.go
    client := NewClient(conn, bufrw, clientID)
    
    ClientManager.Receiver <- client
    
    // The manager will handle everything from this point on
    return
}

func Initialize() {
    // Call auth init here since it depends on some other initializions
    Init_auth()

    mux := http.NewServeMux()
    mux.HandleFunc("/", mainHandler)

    server := http.Server{Addr: config.ServerAddress,
                          Handler: mux,
                          ReadTimeout: time.Second*10,
                          WriteTimeout: time.Second*5}
    server.ListenAndServe()
}