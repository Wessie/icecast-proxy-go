package server

import (
    "github.com/Wessie/icecast-proxy-go/config"
    "github.com/Wessie/icecast-proxy-go/shout"
    "time"
    "fmt"
)

var ClientManager *Manager
var metaStoreTicker <-chan time.Time

func init() {
    metaStoreTicker = time.Tick(time.Second * 5)
    
    ClientManager = NewManager()
    // Start the loop to handle new clients.
    go ClientManager.ProcessClients()
}

func (self *Manager) ProcessClients() {
    dataChan := make(chan *DataPack, 1024)
    errChan := make(chan *ErrPack, 512)
    
    for {
        select {
            case data := <-dataChan:
                // We received some data, handle it.
                mount, ok := self.Mounts[data.Client.ClientID.Mount]
                // Make sure our mount exists at all!
                if !ok {
                    // No mountpoint exist, just ditch the data
                    continue
                }
                
                // This is a pointer comparison, please keep that in mind.
                if mount.Active == data.Client.ClientID {
                    // Active mount, and data HANDLE IT!
                    mount.HandleData(data)
                }
                
                // It's an non-active client otherwise so discard the data silently
            case err := <-errChan:
                // Received an error from the data readers.
                // Get rid of the client!
                self.RemoveClient(err.Client)
                // We should log the error here
                // TODO: Add logging
            case client := <-self.Receiver:
                fmt.Println("manager: new client received.")
                // A new client, let a different function handle
                // the preparations required.
                err := self.AddClient(client)
                
                if err != nil {
                    // This means something is bad, reject the client.
                    self.RemoveClient(client)
                    // TODO: Add logging
                } else {
                    // We are done preparing, start reading.
                    go ReadInto(client, dataChan, errChan)
                }
            case mount := <-self.MountCollector:
                // The mount is 'empty' we have to do some checks and clean up
                // if neccesary
                if len(mount.Clients) > 0 {
                    // The mount got a new client while waiting, ignore it.
                    continue
                }
                // no new clients so we have to clean it up.
                
                // Close our connection to the server
                err := mount.Shout.Close()
                if err != nil {
                    // Log the error but don't do anything with it other than that
                    // TODO: Add logging
                }
                
                // Delete it from our mapping
                delete(self.Mounts, mount.Mount)
                
                // Yay garbage collection! The C bindings need some
                // extra help though, the rest will be done by the
                // garbage collector
                DestroyMount(mount)
            case meta := <-self.MetaChan:
                // Receiving metadata is slightly complicated because our
                // only method to knowing if something is for a specific
                // client is by comparing collected variables that we hope
                // generate a unique ID for the client. The ClientID.Hash
                // method is here for this specific cause.
                fmt.Println("Received a meta packet")
                
                // Pre compute, since we are bound to use it more than once
                // in the rest of this block.
                meta_hash := meta.ID.Hash()
                
                mount, ok := self.Mounts[meta.ID.Mount]
                
                if !ok {
                    // There is no mountpoint known with the name requested by
                    // the one sending the metadata. We save it temporarily.
                    // TODO: Saving
                    fmt.Println("No mountpoint? strange")
                    self.metaStore[meta_hash] = meta.Data
                    continue
                }
                
                // Pre compute, since we are bound to use it more than once
                // in the rest of this block.
                active_hash := mount.Active.Hash()
                
                fmt.Println(active_hash, meta_hash)
                fmt.Println(mount.Active, meta.ID)
                // We have a mountpoint with the name, but first have to check
                // if the active client is sending data or just one of the other
                // connected ones is.
                
                if active_hash != meta_hash {
                    // This means it's one of the other clients sending metadata
                    // Save the metadata for them for when the Active client leaves
                    // TODO: Implement
                    fmt.Println("Not active client")
                    if client, ok := mount.Clients[meta_hash]; ok {
                        client.Metadata = meta.Data
                    } else {
                        // We don't seem to have an actual client connected with
                        // this specific identifier... Discard?
                        // TODO: Check if discarding isn't needed...
                    }
                    continue
                }
                
                // The active client is sending metadata, we don't have to do much
                // special for this case, just send it along to icecast and save the
                // metadata in the Client struct.
                
                client, ok := mount.Clients[active_hash]
                
                if !ok {
                    // We... don't seem to have the active client? This should
                    // be absolutely impossible, lets panic!
                    panic("Active client isn't available.")
                }
                
                // Set our metadata, this is mostly done for info gathering by other
                // code. We don't actually use this value in the client server code.
                client.Metadata = meta.Data
                
                // And send the metadata, we are ignoring errors here
                // TODO: Check if ignoring errors could lead to problems.
                go func() {
                    time.Sleep(time.Second)
                    err := mount.Shout.SendMetadata(meta.Data)
                    if err != nil {
                        fmt.Println(err)
                    }
                }()
            case <-metaStoreTicker:
                // We store metadata for unknown mounts in this mapping.
                // We recreate it every few seconds since we don't want old data
                self.metaStore = make(map[ClientHash]string, 5)
        }
    }
}

func (self *Mount) HandleData(data *DataPack) {
    /* Sends the data in the packet to the icecast server */
    
    // First check if we are connected at all
    if !self.Shout.Connected() {
        err := self.Shout.Open()
        if err != nil {
            // Error occured while connecting, we ditch the data and retry
            // on the next package
            fmt.Println(err)
            return
        }
    }
    
    err := self.Shout.Send(data.Data)
    
    if err != nil {
        // An error occured while sending data, we ditch the data and retry
        // on the next package
        if e, ok := err.(shout.ShoutError); ok {
            if e.Errno == shout.ERR_INSANE {
                // We did something horrible wrong. We have to suicide
                // TODO: Do this cleaner!
                panic("We are insane.")
            } else if e.Errno == shout.ERR_MALLOC {
                // We ran out of memory.. an actual reason to panic!
                panic("Out of memory")
            } else {
                // We can safely assume this means there was a network issue.
                // ditch the current package. Someone else will notice the same
                // issue very soon most likely.
                // TODO: Make sure this can't cause issues
                self.Shout.Close()
                return
            }
        }
    }
}

func (self *Manager) RemoveClient(client *Client) {
    /* Removes a client from the mount point and prepares it for
    deletion.
    
    If no clients are left on this mountpoint the mount will be
    cleaned up. */
    fmt.Println("Removing client!")
    mountName := client.ClientID.Mount
    
    mount, ok := self.Mounts[mountName]
    
    if !ok {
        panic("Unexisting mountpoint")
    }
    
    // First swap out the active client if we have another client
    // connected already.
    select {
        case mount.Active = <-mount.ClientQueue:
            fmt.Println("Switched active streamer")
            c, ok := mount.Clients[mount.Active.Hash()]
            if !ok {
                // Why are we switching to this client if the client doesn't exist?
                // Ah well just ignore it
                // TODO: Check for possible bugs
            }
            
            // We go the easy way out and send the meta into a round trip!
            self.MetaChan <- &MetaPack{c.Metadata, c.ClientID}
        default:
            // Default clause so that the select doesn't hang.
            // Removing this is equal to deathlocking, don't!
    }
    
    // Remove it from the mount map, this is our first action
    delete(mount.Clients, client.ClientID.Hash())
    
    // We have to close the connection ourself since we Hijacked it
    client.Conn.Close()
    
    if len(mount.Clients) == 0 {
        // Register the mount for a collection, we don't collect it here
        // right away because it's common for two sources to overlap or
        // swap each other out with a very small delay. This gives it a
        // small window to reuse the libshout instance and connection.
        go func(collector chan<- *Mount) {
            // Sleep for 5 seconds to give a proper window size.
            time.Sleep(time.Second * 5)
            collector <- mount
        }(self.MountCollector)
    }
}

func (self *Manager) AddClient(client *Client) error {
    /* Adds a client to the respective mount point, if no mount
    point with the given name currently exist a new one is created */
    fmt.Println("Adding client")
    mountName := client.ClientID.Mount
    
    mount, ok := self.Mounts[mountName]
    
    if !ok {
        fmt.Println("No mountpoint exists, creating one.")
        // We don't have a mount yet so we create our own
        mount := NewMount(mountName)
        
        // Don't forget to add ourself to the mount map
        self.Mounts[mountName] = mount
        
        // Add our new client
        mount.Clients[client.ClientID.Hash()] = client
        
        // Since this is a new mount we can set the just added
        // stream as active
        mount.Active = client.ClientID
        
        // We might have saved metadata for this client. Check the storage
        if meta, ok := self.metaStore[mount.Active.Hash()]; ok {
            // We cheat again to not duplicate any code! Just send it back into
            // the processor.
            self.MetaChan <- &MetaPack{meta, client.ClientID}
        }
        
        // Don't forget to change the mountname to the client supplied one
        mount.Shout.ApplyOptions(map[string] string {"mount": mountName})

        // We don't open the connection here because that is handled in the
        // data sending function instead. This keeps the logic simple when
        // potential disconnects or network issues are involved.
        fmt.Println("Finished creating mount")
        return nil
    }
    // Mount already exists so all we have to do is add our new client to it.
    mount.Clients[client.ClientID.Hash()] = client
    
    // We want to make sure we don't deadlock if the client queue is full already.
    if len(mount.ClientQueue) < config.QUEUE_LIMIT {
        // And push the client onto the queue
        mount.ClientQueue <- client.ClientID
    } else {
        return &FullQueue{}
    }
    return nil
}

func ReadInto(client *Client, dataChan chan<- *DataPack, errChan chan<- *ErrPack) {
    for {
        data := make([]byte, config.BUFFER_SIZE)
        
        client.Conn.SetReadDeadline(time.Now().Add(config.Timeout))
        len, err := client.Bufrw.Read(data)
        if err != nil {
            // On any errors we just push it onto the error channel
            // The manager will handle it correctly
            errChan <- &ErrPack{err, client}
            return
        }
        
        dataChan <- &DataPack{data[:len], client}
    }
}