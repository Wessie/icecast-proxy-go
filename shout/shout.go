package shout

import (
    "unsafe"
    "strconv"
)

/*
#cgo LDFLAGS: -lshout
#include "shout/shout.h"
#include <stdlib.h>
*/
import "C"

type Shout struct {
    shout *C.shout_t
    shout_metadata *C.shout_metadata_t
}

type ShoutError struct {
    Errno int
    ErrStr string
}

func (self ShoutError) Error() string {
    return self.ErrStr
}

var Protocols = map[string] C.uint {
    "HTTP": C.SHOUT_PROTOCOL_HTTP,
    "XAUDIOCAST": C.SHOUT_PROTOCOL_XAUDIOCAST,
    "ICY": C.SHOUT_PROTOCOL_ICY,
}
var Formats = map[string] C.uint {
    "OGG": C.SHOUT_FORMAT_OGG,
    "MP3": C.SHOUT_FORMAT_MP3,
}
// Due to some type conversions this is an ugly hack to make it easier
var AudioParams = map[string] string {
    "bitrate": C.SHOUT_AI_BITRATE,
    "channels": C.SHOUT_AI_CHANNELS,
    "samplerate": C.SHOUT_AI_SAMPLERATE,
    "quality": C.SHOUT_AI_QUALITY,
}

func init() {
    C.shout_init()
}



func NewShout(options map[string] string) *Shout {
    /* Creates a new Shout instance
    
    options is a mapping of settings to pass to the new Shout instance
    on creation. The following settings are supported.
    
    All settings should be strings, they are converted accordingly when
    being set.
    
    metadata: The initial metadata to send to the server.
    host: The server hostname or IP. Default is localhost
    port: The server port. Default is 8000
    user: The user to authenticate as. The default is source
    passwd: The password to authenticate with. No default
    protocol: The protocol to use (Listed above). Default is HTTP
    format: The format to use (Listed above). Default is VORBIS
    mount: The mountpoint for this stream. No default
           (Only available if protocol supports it)
    dumpfile: If the server supports it, you can request that 
              your stream be archived on the server under the 
              name dumpfile. This can quickly eat a lot of disk
              space, so think twice before setting it. 
    agent: The useragent that is send to the server on connecting.
            (Defaults to libshout/version)
    
    Optional directory parameters:
    
    public: bool indicating if the stream should be published on any
            directories the server knows about.
    name: The name of the stream.
    url: An URL for the stream.
    genre: A genre for the stream.
    description: A description for the stream.
    
    Audio parameters:
    
    bitrate: Sets the audio bitrate.
    samplerate: Sets the audio sample rate.
    channels: The amount of channels in the audio.
    quality: A quality setting of the audio.
    */
    
    charset := C.CString("charset")
    charset_option := C.CString("UTF8")
    
    // FREE THEM, libshout copies them over anyway.
    defer C.free(unsafe.Pointer(charset))
    defer C.free(unsafe.Pointer(charset_option))
    
    // Create new C libshout struct
    shout_t := C.shout_new()
    
    shout_metadata_t := C.shout_metadata_new()
    C.shout_metadata_add(shout_metadata_t, charset, charset_option)
    
    new := Shout{shout_t, shout_metadata_t}
    // Set the options we got passed
    new.ApplyOptions(options)
    
    return &new
}

func DestroyShout(shout Shout) {
    /* Destroys the Shout instance. The instance can't be used
    without undefined behaviour after calling this function */
    C.shout_free(shout.shout)
    C.shout_metadata_free(shout.shout_metadata)
}

func (self *Shout) ApplyOptions(options map[string] string) error {
    for key, value := range options {
        option := C.CString(value)
        defer C.free(unsafe.Pointer(option))
        
        switch key {
            case "host":
                C.shout_set_host(self.shout, option)
            case "port":
                temp, err := strconv.Atoi(value)
                if err != nil {
                    continue
                }
                
                port := (C.ushort)(temp)
                C.shout_set_port(self.shout, port)
            case "user":
                C.shout_set_user(self.shout, option)
            case "passwd":
                C.shout_set_password(self.shout, option)
            case "protocol":
                proto := Protocols[value]
                C.shout_set_protocol(self.shout, proto)
            case "format":
                format := Formats[value]
                C.shout_set_format(self.shout, format)
            case "mount":
                C.shout_set_mount(self.shout, option)
            case "dumpfile":
                C.shout_set_dumpfile(self.shout, option)
            case "agent":
                C.shout_set_agent(self.shout, option)
            case "public":
                public := (C.uint)(0)
                if value == "true" || value == "1" {
                    public = (C.uint)(1)
                }
                C.shout_set_public(self.shout, public)
            case "name":
                C.shout_set_name(self.shout, option)
            case "url":
                C.shout_set_url(self.shout, option)
            case "genre":
                C.shout_set_genre(self.shout, option)
            case "description":
                C.shout_set_description(self.shout, option)
            case "bitrate", "samplerate", "channels", "quality":
                param := AudioParams[value]
                ctype := C.CString(param)
                C.shout_set_audio_info(self.shout, ctype,
                                       option)
        }
    }
    return nil
}

func (self *Shout) Open() error {
    /* Opens the shout instance */
    err := C.shout_open(self.shout)
    if err != 0 {
        return self.createShoutError()
    }
    return nil
}

func (self *Shout) Close() error {
    err := C.shout_close(self.shout)
    if err != 0 {
        return self.createShoutError()
    }
    return nil
}

func (self *Shout) Send(data []byte) (err error) {
    length := len(data)

    res := int(C.shout_send(self.shout,
                        (*C.uchar)(unsafe.Pointer(&data[0])),
                        (C.size_t)(length)))
    if res != C.SHOUTERR_SUCCESS {
        return self.createShoutError()
    }
    return nil
}

func (self *Shout) SendMetadata(meta string) (error) {
    /* Updates the metadata. This is only supported for MP3 streams */
    new_string := C.CString(meta)
    song_string := C.CString("song")
    // Make sure we call free at the end
    defer C.free(unsafe.Pointer(new_string))
    defer C.free(unsafe.Pointer(song_string))
    
    i := C.shout_metadata_add(self.shout_metadata, song_string, new_string)
    if i != C.SHOUTERR_SUCCESS {
        return self.createShoutError()
    }
    
    i = C.shout_set_metadata(self.shout, self.shout_metadata)
    if i != C.SHOUTERR_SUCCESS {
        return self.createShoutError()
    }
    
    return nil
}

func (self *Shout) Connected() bool {
    value := C.shout_get_connected(self.shout)
    if value == C.SHOUTERR_CONNECTED {
        return true
    }
    return false
}

func (self *Shout) Sync() {
    C.shout_sync(self.shout)
}

func (self *Shout) Delay() int {
    return int(C.shout_delay(self.shout))
}

func (self *Shout) createShoutError() ShoutError {
    /* Creates a Go error of the C library */
    errno := int(C.shout_get_errno(self.shout))
    errstr := C.GoString(C.shout_get_error(self.shout))
    return ShoutError{errno, errstr}
}

const (
    ERR_SUCCESS = C.SHOUTERR_SUCCESS
    ERR_INSANE = C.SHOUTERR_INSANE
    ERR_MALLOC = C.SHOUTERR_MALLOC
    ERR_NOCONNECT = C.SHOUTERR_NOCONNECT
    ERR_NOLOGIN = C.SHOUTERR_NOLOGIN
    ERR_SOCKET = C.SHOUTERR_SOCKET
    ERR_METADATA = C.SHOUTERR_METADATA
    ERR_CONNECTED = C.SHOUTERR_CONNECTED
    ERR_UNCONNECTED = C.SHOUTERR_UNCONNECTED
    ERR_UNSUPPORTED = C.SHOUTERR_UNSUPPORTED
)