package server

type Manager struct {
	/* A construct that contains the state used by the
	   managing of the source client connections */
	Mounts map[string]*Mount
	// A channel to receive new clients from
	Receiver chan *Client
	// A channel that allows to register mounts as empty
	// this way we can clean them up outside client logic.
	MountCollector chan *Mount
	// A channel to receive metadata on
	MetaChan chan *MetaPack
	// This is a mapping to store a temporary metadata copy
	metaStore map[ClientHash]string
}

func NewManager() *Manager {
	mounts := make(map[string]*Mount, 5)
	receiver := make(chan *Client, 5)
	collector := make(chan *Mount, 5)
	meta := make(chan *MetaPack, 10)
	metastore := make(map[ClientHash]string, 5)

	return &Manager{Mounts: mounts,
		Receiver:       receiver,
		MountCollector: collector,
		MetaChan:       meta,
		metaStore:      metastore}
}

func DestroyManager(self *Manager) {
	// Close all the channels we have
	close(self.Receiver)
	close(self.MountCollector)
	close(self.MetaChan)

	// metaStore will be garbage collected, no need to get rid of it

	// The list of mounts need a cleanup properly though, since we can't
	// assume the main loop is running we don't use any of the pre-existing
	// cleanup methods
	for _, mount := range self.Mounts {
		DestroyMount(mount)
	}
}
