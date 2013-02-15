package server

import (
    "github.com/Wessie/icecast-proxy-go/config"
    "github.com/Wessie/icecast-proxy-go/shout"
    "time"
    "fmt"
)

var ClientManager *Manager
var timeout chan int


func init() {
    // Make sure we initialize our timeout channel
    timeout = make(chan int, 2)
    // And add the first value!
    timeout <- 1

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
        case <-timeout:
            timeout <- 1
    }
    
    // Remove it from the mount map, this is our first action
    delete(mount.Clients, client.ClientID)
    
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
    
    fmt.Println("Checking mount existance.")
    mount, ok := self.Mounts[mountName]
    
    if !ok {
        fmt.Println("No mountpoint exists, creating one.")
        // We don't have a mount yet so we create our own
        mount := NewMount(mountName)
        
        // Don't forget to add ourself to the mount map
        self.Mounts[mountName] = mount
        
        // Add our new client
        fmt.Println("Adding client to mapping")
        mount.Clients[client.ClientID] = client
        
        // Since this is a new mount we can set the just added
        // stream as active
        fmt.Println("Setting active client.")
        mount.Active = client.ClientID
        
        // Don't forget to change the mountname to the client supplied one
        fmt.Println("Setting mount option")
        mount.Shout.ApplyOptions(map[string] string {"mount": mountName})

        // We don't open the connection here because that is handled in the
        // data sending function instead. This keeps the logic simple when
        // potential disconnects or network issues are involved.
        fmt.Println("Finished creating mount")
        return nil
    }
    // Mount already exists so all we have to do is add our new client to it.
    mount.Clients[client.ClientID] = client
    
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