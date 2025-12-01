package kv

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"unsafe"

	"kv-rdma/kv_rdma/rdma"
)

// ServerConnection holds RDMA resources for a single server connection.
type ServerConnection struct {
	qp         *rdma.QueuePair
	cq         *rdma.CompletionQueue
	serverInfo HandshakeInfo
}

// Client represents the KV store client.
type Client struct {
	rdmaCtx     *rdma.Context
	mr          *rdma.MemoryRegion
	localBuf    []byte
	connections []*ServerConnection // Slice for easy indexing by hash
	port        int
	gidIndex    int
}

// NewClient creates a new client.
func NewClient(devName string, port, gidIndex int) (*Client, error) {
	ctx, err := rdma.NewContext(devName)
	if err != nil {
		return nil, fmt.Errorf("failed to create RDMA context: %v", err)
	}

	// Local buffer for RDMA operations
	// Increase to 1MB to support batching
	localBuf := make([]byte, 1024*1024)
	mr, err := ctx.RegisterMR(unsafe.Pointer(&localBuf[0]), len(localBuf),
		0x1|0x2|0x4) // LOCAL_WRITE | REMOTE_WRITE | REMOTE_READ
	if err != nil {
		return nil, fmt.Errorf("failed to register MR: %v", err)
	}

	return &Client{
		rdmaCtx:  ctx,
		mr:       mr,
		localBuf: localBuf,
		port:     port,
		gidIndex: gidIndex,
	}, nil
}

// Connect connects to all servers.
func (c *Client) Connect(serverAddrs []string) error {
	for _, addr := range serverAddrs {
		conn, err := c.connectToServer(addr)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %v", addr, err)
		}
		c.connections = append(c.connections, conn)
	}
	return nil
}

func (c *Client) connectToServer(serverAddr string) (*ServerConnection, error) {
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial server: %v", err)
	}
	defer conn.Close()

	// 1. Create QP
	cq, err := c.rdmaCtx.CreateCQ(128)
	if err != nil {
		return nil, fmt.Errorf("failed to create CQ: %v", err)
	}

	qp, err := c.rdmaCtx.CreateQP(cq)
	if err != nil {
		return nil, fmt.Errorf("failed to create QP: %v", err)
	}

	// 2. Init QP
	if err := qp.InitQP(c.port); err != nil {
		return nil, fmt.Errorf("failed to Init QP: %v", err)
	}

	// 3. Get Local Info
	portAttr, err := c.rdmaCtx.QueryPort(c.port)
	if err != nil {
		return nil, fmt.Errorf("failed to query port: %v", err)
	}

	gid, err := c.rdmaCtx.QueryGID(c.port, c.gidIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to query gid: %v", err)
	}

	myInfo := HandshakeInfo{
		QPN:  qp.QPN(),
		LID:  portAttr.LID,
		PSN:  5678, // Random PSN
		RKey: c.mr.RKey(),
		Addr: c.mr.Addr(),
		GID:  gid,
	}

	// 4. Send Client Info
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(myInfo); err != nil {
		return nil, fmt.Errorf("failed to encode client info: %v", err)
	}

	// 5. Receive Server Info
	var serverInfo HandshakeInfo
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&serverInfo); err != nil {
		return nil, fmt.Errorf("failed to decode server info: %v", err)
	}

	// 6. RTR
	if err := qp.RtrQP(serverInfo.QPN, serverInfo.LID, serverInfo.PSN, serverInfo.GID, c.port, c.gidIndex); err != nil {
		return nil, fmt.Errorf("failed to RTR: %v", err)
	}

	// 7. RTS
	if err := qp.RtsQP(myInfo.PSN); err != nil {
		return nil, fmt.Errorf("failed to RTS: %v", err)
	}

	return &ServerConnection{
		qp:         qp,
		cq:         cq,
		serverInfo: serverInfo,
	}, nil
}

// getServer returns the connection for the given key (sharding).
func (c *Client) getServer(key int) *ServerConnection {
	// Simple modulo sharding
	idx := key % len(c.connections)
	return c.connections[idx]
}

// Put writes data to the server.
func (c *Client) Put(key int, data []byte) error {
	// This Put function is designed for a specific KV layout where key and value are written separately.
	// It assumes key is an int, and data is the value.
	// The remote memory layout is assumed to be: [key (64 bytes)][value (128 bytes)] per bucket.
	// The local buffer is used to stage both key and value.

	// Convert key to byte slice for hashing and writing
	keyBytes := []byte(fmt.Sprintf("%d", key))
	if len(keyBytes) > 64 {
		return fmt.Errorf("key too large (max 64 bytes)")
	}
	if len(data) > 128 {
		return fmt.Errorf("value too large (max 128 bytes)")
	}

	// Ensure local buffer is large enough for key (64) + value (128)
	if 64+128 > len(c.localBuf) {
		return fmt.Errorf("local buffer too small for key and value")
	}

	// Stage data in local buffer
	copy(c.localBuf[0:len(data)], data)

	// Calculate remote address: Base + Key
	// For simplicity in this benchmark, we assume key is the offset
	remoteOffset := uint64(key)

	// 1. Post Write (Key)
	// We just write the value directly for this simple test, ignoring the key/value split logic
	// to match Get/PutBatch behavior which treats key as offset.
	// Actually, PutBatch writes 'val' to 'remoteAddr'.
	// So Put should do the same.

	conn := c.getServer(key)
	remoteAddr := conn.serverInfo.Addr + remoteOffset

	// Post Write
	wrID1 := uint64(1)
	err := conn.qp.PostWrite(c.mr, 0, len(data), remoteAddr, conn.serverInfo.RKey, wrID1, rdma.IBV_SEND_SIGNALED)
	if err != nil {
		return fmt.Errorf("failed to post write: %v", err)
	}
	log.Printf("Posted Write. ID: %d", wrID1)

	// Wait for completion
	for {
		id, err := conn.cq.PollCQ()
		if err != nil {
			return err
		}
		if id == wrID1 {
			break
		}
	}
	return nil
}

// PutBatch writes a batch of data to servers.
func (c *Client) PutBatch(keys []int, data [][]byte) error {
	if len(keys) != len(data) {
		return fmt.Errorf("keys and data length mismatch")
	}

	// 1. Post all writes using Batch Posting
	serverOps := make(map[*ServerConnection]int)
	serverItems := make(map[*ServerConnection][]rdma.WriteItem)

	bufOffset := 0
	for i, key := range keys {
		val := data[i]
		if bufOffset+len(val) > len(c.localBuf) {
			return fmt.Errorf("batch too large for local buffer")
		}
		copy(c.localBuf[bufOffset:], val)

		conn := c.getServer(key)
		remoteAddr := conn.serverInfo.Addr + uint64(key)

		item := rdma.WriteItem{
			Offset:     bufOffset,
			Length:     len(val),
			RemoteAddr: remoteAddr,
			RKey:       conn.serverInfo.RKey,
			WrID:       uint64(i + 1),
		}
		serverItems[conn] = append(serverItems[conn], item)
		bufOffset += len(val)
	}

	for conn, items := range serverItems {
		// Signal only the last one (PostWriteBatch handles this flag logic internally if we pass SIGNALED)
		err := conn.qp.PostWriteBatch(c.mr, items, rdma.IBV_SEND_SIGNALED)
		if err != nil {
			return fmt.Errorf("failed to post batch write: %v", err)
		}
		serverOps[conn] = 1 // We expect 1 completion per batch
	}

	// 2. Wait for completions
	// We expect 1 completion per server involved
	totalOps := len(serverOps)
	completed := 0

	for completed < totalOps {
		progress := false
		for conn := range serverOps {
			for {
				id, err := conn.cq.PollCQ()
				if err != nil {
					return err
				}
				if id > 0 {
					completed++
					progress = true
					// Since we only get 1 completion per batch, we can break after getting it.
					break
				} else {
					break
				}
			}
		}

		if completed >= totalOps {
			break
		}

		if !progress {
			// Busy poll
		}
	}
	return nil
}

// Get reads data from the server.
func (c *Client) Get(key int, size int) ([]byte, error) {
	if size > len(c.localBuf) {
		return nil, fmt.Errorf("size too large")
	}

	conn := c.getServer(key)
	offset := key
	remoteAddr := conn.serverInfo.Addr + uint64(offset)

	// RDMA Read
	err := conn.qp.PostRead(c.mr, 0, size, remoteAddr, conn.serverInfo.RKey, 2, rdma.IBV_SEND_SIGNALED)
	if err != nil {
		return nil, fmt.Errorf("failed to post read: %v", err)
	}

	// Wait for completion
	for {
		id, err := conn.cq.PollCQ()
		if err != nil {
			return nil, err
		}
		if id == 2 {
			break
		}
	}

	result := make([]byte, size)
	copy(result, c.localBuf[:size])
	return result, nil
}

// GetBatch reads a batch of data.
func (c *Client) GetBatch(keys []int, size int) ([][]byte, error) {
	results := make([][]byte, len(keys))
	serverOps := make(map[*ServerConnection]int)
	bufOffset := 0

	for i, key := range keys {
		if bufOffset+size > len(c.localBuf) {
			return nil, fmt.Errorf("batch too large for local buffer")
		}

		conn := c.getServer(key)
		remoteAddr := conn.serverInfo.Addr + uint64(key)

		err := conn.qp.PostRead(c.mr, bufOffset, size, remoteAddr, conn.serverInfo.RKey, uint64(i+1), rdma.IBV_SEND_SIGNALED)
		if err != nil {
			return nil, fmt.Errorf("failed to post read: %v", err)
		}

		serverOps[conn]++
		bufOffset += size
	}

	// Poll and collect results
	totalOps := len(keys)
	completed := 0

	for completed < totalOps {
		// 1. Poll all CQs first
		progress := false
		for conn := range serverOps {
			for {
				id, err := conn.cq.PollCQ()
				if err != nil {
					return nil, err
				}
				if id > 0 {
					completed++
					progress = true
				} else {
					break
				}
			}
		}

		if completed >= totalOps {
			break
		}

		// 2. If no progress, just yield
		if !progress {
			// Busy poll
		}
	}

	// Copy results
	bufOffset = 0
	for i := range keys {
		val := make([]byte, size)
		copy(val, c.localBuf[bufOffset:bufOffset+size])
		results[i] = val
		bufOffset += size
	}

	return results, nil
}

// Send sends data to the server using RDMA Send.
func (c *Client) Send(key int, val []byte) error {
	conn := c.getServer(key)

	// Copy data to local buffer
	copy(c.localBuf, val)

	// Post Send
	// wr_id = 1
	if err := conn.qp.PostSend(c.mr, 0, len(val), 1, rdma.IBV_SEND_SIGNALED); err != nil {
		return fmt.Errorf("failed to post send: %v", err)
	}

	// Poll for completion
	for {
		id, err := conn.cq.PollCQ()
		if err != nil {
			return err
		}
		if id > 0 {
			break
		}
		// Busy poll
	}
	return nil
}

// Write writes data to the server using RDMA Write.
func (c *Client) Write(key int, val []byte) error {
	conn := c.getServer(key)
	remoteAddr := conn.serverInfo.Addr + uint64(key)

	// Copy data to local buffer
	copy(c.localBuf, val)

	// Post Write
	if err := conn.qp.PostWrite(c.mr, 0, len(val), remoteAddr, conn.serverInfo.RKey, 1, rdma.IBV_SEND_SIGNALED); err != nil {
		return fmt.Errorf("failed to post write: %v", err)
	}

	// Poll for completion
	for {
		id, err := conn.cq.PollCQ()
		if err != nil {
			return err
		}
		if id > 0 {
			break
		}
		// Busy poll
	}
	return nil
}

// Close closes the client.
func (c *Client) Close() {
	for _, conn := range c.connections {
		if conn.qp != nil {
			conn.qp.Destroy()
		}
		if conn.cq != nil {
			conn.cq.Destroy()
		}
	}
	if c.mr != nil {
		c.mr.Deregister()
	}
	if c.rdmaCtx != nil {
		c.rdmaCtx.Close()
	}
}
