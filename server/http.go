package server

/*
Implements the HTTP handlers for the icecast server.

This file contains all HTTP handlers and their respective needs.
The actual icecast data handling is done in the 'icecast.go' file.

while this file does a relatively big part of the icecast protocol it should
try to do as little as possible when it comes to the protocol.
*/

import (
	"github.com/Wessie/icecast-proxy-go/config"
	"github.com/Wessie/icecast-proxy-go/http"
	"fmt"
	"io"
	"strconv"
	"time"
)

var AdminHTML string = `
<html><head><title>Icecast Proxy</title>
<style type="text/css">
table{border: 1px solid #999;border-right:0;border-bottom:0;margin-top:4px;}
td, th{border-bottom:1px solid #ccc;border-right:1px solid #eee;padding: .2em .5em;}
form{margin:0;padding:0;}
</style></head>
<body>
<h3>Icecast Proxy</h3>
%s
</body></html>
`

var MountHTML string = `
<table width="800px" cellspacing="0" cellpadding="2">
<tr><th align="left" colspan="5">%s</th></tr>
<tr><th width="80px">Username</th>
<th>Metadata</th>
<th width="150px">Useragent</th>
<th width="50px">Kick</th></tr>
%s
</table>
`

var ClientHTML string = `
<tr>
<td>%s &nbsp;</td>
<td>%s &nbsp;</td>
<td>%s &nbsp;</td>
<td>
<form action="/admin/kick" method="GET">
<input type="hidden" name="mount" value="%s" />
<input type="hidden" name="num" value="%d" />
<input type="submit" value="Kick" %s />
</form></td></tr>
`


/*
adminHandler is called whenever the /admin URL is requested. The two URLs
special cased for metadata and listener listing respectively are not included
and are handled by different handlers.
*/
func adminHandler(w http.ResponseWriter, r *http.Request, clientID *ClientID) {
	HandlerLock.Lock()
	if r.URL.Path == "/admin" {
		Body := ""
		for mount, clients := range HandlerMounts {
			MountBody := ""
			for i, c := range clients {
				name := c.ClientID.Name
				if i == 0 {
					name = fmt.Sprintf("<b>%s</b>", c.ClientID.Name)
				}
				ClientBody := fmt.Sprintf(ClientHTML, name, c.Metadata, c.ClientID.Agent, mount, i, "")
				MountBody = MountBody + ClientBody
			}
			Body = Body + fmt.Sprintf(MountHTML, mount, MountBody)
		}
		w.Write([]byte(fmt.Sprintf(AdminHTML, Body)))
	} else if r.URL.Path == "/admin/kick" {
		MountName := r.URL.Query().Get("mount")
		Id, err := strconv.Atoi(r.URL.Query().Get("num"))
		if err == nil {
			clients, ok := HandlerMounts[MountName]
			if ok {
				clients[Id].Conn.Close()
			}
		}
		w.Header().Set("Location", "/admin")
		w.WriteHeader(301)
	}
	HandlerLock.Unlock()
}

/*
The root handler of the server, this handler exists because we have
to determine if something is a SOURCE request or a GET request before
sending it to the correct handler.
*/
func mainHandler(w http.ResponseWriter, r *http.Request) {
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

	// Sending empty metadata is useless, so we don't
	if meta != "" {
		// And we are done here, send the data we have so far along
		ClientManager.MetaChan <- &MetaPack{Data: meta, ID: clientID, Seen: false}
	}

	response := []byte("<?xml version=\"1.0\"?>\n<iceresponse><message>Metadata update successful</message><return>1</return></iceresponse>\n")

	w.Header().Set("Content-Length", strconv.Itoa(len(response)))
	w.Write(response)
}

/*
sourceHandler is the handler for icecast source clients. It acknowledges the
client before sending it over to the icecast manager.
*/
func sourceHandler(w http.ResponseWriter, r *http.Request, clientID *ClientID) {
	/* Handler for icecast source requests. This can only be called by
	   authenticated requests */

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
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	// We don't use the real functionality of the Muxer because we require to
	// differentiate between GET/POST and SOURCE requests.
	mux.HandleFunc("/", mainHandler)

	server := http.Server{Addr: config.ServerAddress,
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 5}
	server.ListenAndServe()
}
