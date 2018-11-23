package server

import (
	"github.com/Wessie/icecast-proxy-go/config"
	"github.com/Wessie/icecast-proxy-go/shout"
	"log"
	"os"
	"time"
)

var ClientManager *Manager
var metaStoreTicker <-chan time.Time
var logger *log.Logger

func init() {
	metaStoreTicker = time.Tick(time.Second * 5)

	logger = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)

	// Start the loop to handle new clients.
	go func() {
		for {
			oldManager := ClientManager
			ClientManager = NewManager()
			if oldManager != nil {
				DestroyManager(oldManager)
			}
			func() {
				defer func() {
					if x := recover(); x != nil {
						log.Printf("run time panic: %v", x)
					}
				}()
				ClientManager.ProcessClients()
			}()
		}
	}()
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
			logger.Printf(":remove client:%s: %s (reason: '%s')",
				err.Client.ClientID.Mount,
				err.Client.String(),
				err.Err.Error())
		case client := <-self.Receiver:
			// A new client, let a different function handle
			// the preparations required.
			err := self.AddClient(client)

			if err != nil {
				// This means something is bad, reject the client.
				self.RemoveClient(client)
				// TODO: Add logging
				logger.Printf(":error adding client: %s (reason: %s)",
					client.String(), err.Error())
			} else {
				// Send the client to the handler, we don't want to send it
				// earlier than this since it could mean there are errors
				// when pre-processing it.
				HandleClientConnect(client)
				// We are done preparing, start reading.
				go ReadInto(client, dataChan, errChan)
			}
		case mount := <-self.MountCollector:
			// The mount is 'empty' we have to do some checks and clean up
			// if neccesary
			logger.Printf(":collecting mount: %s", mount.Mount)

			if mount.Clients.Length > 0 {
				// The mount got a new client while waiting, ignore it.
				logger.Printf(":collection aborted: %s", mount.Mount)
				continue
			}
			// no new clients so we have to clean it up.

			// Close our connection to the server
			logger.Printf(":icecast disconnect: %s", mount.Mount)
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
			logger.Printf(":collection finished: %s", mount.Mount)
		case meta := <-self.MetaChan:
			// Receiving metadata is slightly complicated because our
			// only method to knowing if something is for a specific
			// client is by comparing collected variables that we hope
			// generate a unique ID for the client. The ClientID.Hash
			// method is here for this specific cause.

			// Pre compute, since we are bound to use it more than once
			// in the rest of this block.
			meta_hash := meta.ID.Hash()

			logger.Printf(":metadata:%x: %s", meta_hash, meta.Data)

			mount, ok := self.Mounts[meta.ID.Mount]

			if !ok {
				// There is no mountpoint known with the name requested by
				// the one sending the metadata. We save it temporarily.
				logger.Printf(":metadata stored: %s", meta.Data)
				self.metaStore[meta_hash] = meta.Data
				continue
			}

			// Pre compute, since we are bound to use it more than once
			// in the rest of this block.
			active_hash := mount.Active.Hash()

			// We have a mountpoint with the name, but first have to check
			// if the active client is sending data or just one of the other
			// connected ones is.

			if active_hash != meta_hash {
				// This means it's one of the other clients sending metadata
				// Save the metadata for them for when the Active client leaves
				if client, ok := mount.Clients.GetByHash(meta_hash); ok {
					client.Metadata = meta.Data

					// Don't forget to call our handler
					HandleMetadata(client, meta.Data)
				} else {
					// We don't seem to have an actual client connected with
					// this specific identifier... Discard?
					logger.Printf(":metadata discarded: %s", meta.Data)
					// TODO: Check if discarding isn't needed...
				}
				continue
			}

			// The active client is sending metadata, we don't have to do much
			// special for this case, just send it along to icecast and save the
			// metadata in the Client struct.

			client, ok := mount.Clients.GetByHash(active_hash)

			if !ok {
				// We... don't seem to have the active client?
				// Lets drop the packet and just continue on!
				continue
			}

			// Set our metadata, this is mostly done for info gathering by other
			// code. We don't actually use this value in the client server code.
			client.Metadata = meta.Data

			// And send the metadata, we are ignoring errors here
			// TODO: Check if ignoring errors could lead to problems.
			if meta.Seen {
				if err := mount.Shout.SendMetadata(meta.Data); err != nil {
					logger.Printf(":metadata failed: %s", err)
				}
			} else {
				go func() {
					time.Sleep(time.Second)
					meta.Seen = true
					self.MetaChan <- meta
				}()

				// Call our handler for metadata, we do it here since we
				// already verified the metadata is fine for sending, there
				// is no need to wait out the extra second.
				HandleMetadata(client, meta.Data)
			}
		case <-metaStoreTicker:
			// We store metadata for unknown mounts in this mapping.
			// We recreate it every few seconds since we don't want old data
			self.metaStore = make(map[ClientHash]string, 5)
		}
	}
}

/* Sends the data in the packet to the icecast server

This function is also the only one responsible for the connection to the
icecast server. It checks the connection status on each data package and
tries to connect when the connection is down. If the connection goes down
between the check and the actual send it will discard the current data
package and rely on the next call to this function to reconnect.
*/
func (self *Mount) HandleData(data *DataPack) {

	// First check if we are connected at all
	if !self.Shout.Connected() {
		// Do a close call to be sure of no lingering connections.
		self.Shout.Close()

		logger.Printf(":icecast connecting: %s", self.Mount)
		err := self.Shout.Open()
		if err != nil {
			logger.Printf(":icecast error: %s (error: %s)", self.Mount, err.Error())
			// Error occured while connecting, we ditch the data and retry
			// on the next package
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

/*
Swaps the current active client with the new client.
*/
func (self *Manager) SwapLiveClient(mount *Mount, client *Client) {
	if mount.Active == client.ClientID {
		// The new client is already the active client.
		return
	}

	// We want to call the handler before swapping them out after all!
	old_live_client, ok := mount.Clients.GetByID(mount.Active)
	if !ok {
		// This shouldn't ever happen! oh boy did we do this before.
		// Set the variable to nil so we can check it later.
		old_live_client = nil
	}

	mount.Active = client.ClientID

	// Call the handlers, the order doesn't really matter
	// Lets first make sure we aren't sending a nil pointer.
	if old_live_client != nil {
		HandleClientUnlive(old_live_client)
	}

	HandleClientLive(client)

	// We found a new client we can switch to. Lets continue the
	// work needed, such as saved metadata.
	self.MetaChan <- &MetaPack{client.Metadata,
		client.ClientID,
		false}
}

/*
Switches to the next available client, this uses the Mount.ClientQueue
for determining what the next client shall be.
*/
func (self *Manager) NextLiveClient(mount *Mount, client *Client) {
client_loop:
	for {
		select {
		case new_id := <-mount.ClientQueue:
			new_client, ok := mount.Clients.GetByID(new_id)
			if !ok || new_client.ClientID != new_id {
				// We seem to have hit an old client. Get rid of it.
				continue
			}

			// Swap the clients out.
			self.SwapLiveClient(mount, new_client)

			break client_loop
		default:
			break client_loop
		}
	}
}

/* Removes a client from the mount point and prepares it for
deletion.

If no clients are left on this mountpoint the mount will be
cleaned up. */
func (self *Manager) RemoveClient(client *Client) {

	mountName := client.ClientID.Mount

	mount, ok := self.Mounts[mountName]

	if !ok {
		panic("Unexisting mountpoint")
	}

	if mount.Active == client.ClientID {
		// Put the next available client live
		self.NextLiveClient(mount, client)
	}

	// Remove it from the mount map.
	mount.Clients.Remove(client)

	// We have to close the connection ourself since we Hijacked it
	client.Conn.Close()

	// This currently is the most logical place to call this since we can
	// be sure the connection is already closed at this point, and thus avoid
	// some potential problems in the handlers.
	HandleClientDisconnect(client)

	if mount.Clients.Length == 0 {
		// Register the mount for a collection, we don't collect it here
		// right away because it's common for two sources to overlap or
		// swap each other out with a very small delay. This gives it a
		// small window to reuse the libshout instance and connection.
		self.MountCollector <- mount
	}
}

/* Adds a client to the respective mount point, if no mount
point with the given name currently exist a new one is created */
func (self *Manager) AddClient(client *Client) (err error) {
	defer func() {
		/* This makes sure we can't panic inside this method */
		if x := recover(); x != nil {
			log.Printf("run time panic: %v", x)
			// We use the full queue error here since that is a known one.
			err = &FullQueue{}
		}
	}()

	logger.Printf(":new client:%s: %s @ %s", client.ClientID.Mount,
		client.ClientID.Name, client.ClientID.Addr)

	mountName := client.ClientID.Mount

	mount, ok := self.Mounts[mountName]

	if !ok {
		logger.Printf(":new mount: %s", client.ClientID.Mount)

		// We don't have a mount yet so we create our own
		mount := NewMount(mountName)

		// Don't forget to add ourself to the mount map
		self.Mounts[mountName] = mount

		// Add our new client
		mount.Clients.Add(client)

		// Since this is a new mount we can set the just added
		// stream as active
		mount.Active = client.ClientID

		// We might have saved metadata for this client. Check the storage
		if meta, ok := self.metaStore[mount.Active.Hash()]; ok {
			// We cheat again to not duplicate any code! Just send it back into
			// the processor.
			self.MetaChan <- &MetaPack{meta, client.ClientID, false}
		}

		var audio_format string
		if client.ClientID.AudioFormat == "" {
			audio_format = "MP3"
		} else {
			audio_format = client.ClientID.AudioFormat
		}

		// Don't forget to change the mountname to the client supplied one
		mount.Shout.ApplyOptions(map[string]string{"mount": mountName,
			"format": audio_format})

		// We don't open the connection here because that is handled in the
		// data sending function instead. This keeps the logic simple when
		// potential disconnects or network issues are involved.
		return nil
	}
	// Mount already exists so all we have to do is add our new client to it.
	mount.Clients.Add(client)

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
	defer func() {
		// Function to protect the rest of the runtime from panics in here.
		// This will send an error to the manager
		if x := recover(); x != nil {
			log.Printf("run time panic: %v", x)
			errChan <- &ErrPack{nil, client}
		}
	}()

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
