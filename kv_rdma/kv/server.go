package kv

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"unsafe"

	"kv-rdma/kv_rdma/rdma"
)

const (
	MemorySize = 1024 * 1024 * 1024 // 1GB
	Port       = ":8080"
)

// Server represents the KV store server.
type Server struct {
	rdmaCtx  *rdma.Context
	mr       *rdma.MemoryRegion
	data     []byte
	port     int
	gidIndex int
}

// NewServer creates a new server.
func NewServer(devName string, port, gidIndex int) (*Server, error) {
	ctx, err := rdma.NewContext(devName)
	if err != nil {
		return nil, fmt.Errorf("failed to create RDMA context: %v", err)
	}

	// Allocate 1GB memory for KV store
	data := make([]byte, 1024*1024*1024)
	mr, err := ctx.RegisterMR(unsafe.Pointer(&data[0]), len(data),
		0x1|0x2|0x4) // LOCAL_WRITE | REMOTE_WRITE | REMOTE_READ (using raw values for now, should export constants)
	// Actually, I should export constants from rdma package or use CGO constants if possible,
	// but since I can't easily access C constants in another package without CGO, I'll use hardcoded values for now.
	// Better: Add constants to rdma package.
	if err != nil {
		return nil, fmt.Errorf("failed to register MR: %v", err)
	}

	return &Server{
		rdmaCtx:  ctx,
		mr:       mr,
		data:     data,
		port:     port,
		gidIndex: gidIndex,
	}, nil
}

// HandshakeInfo contains QP and MR info.
type HandshakeInfo struct {
	QPN  uint32
	LID  uint16
	PSN  uint32
	RKey uint32
	Addr uint64
	GID  []byte
}

// Start starts the server.
func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP: %v", err)
	}
	log.Printf("Server listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// 1. Create QP
	cq, err := s.rdmaCtx.CreateCQ(128)
	if err != nil {
		log.Printf("Failed to create CQ: %v", err)
		return
	}
	qp, err := s.rdmaCtx.CreateQP(cq)
	if err != nil {
		log.Printf("Failed to create QP: %v", err)
		return
	}

	// 2. Init QP
	if err := qp.InitQP(s.port); err != nil {
		log.Printf("Failed to Init QP: %v", err)
		return
	}

	// 3. Receive Client Info
	var clientInfo HandshakeInfo
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&clientInfo); err != nil {
		log.Printf("Failed to decode client info: %v", err)
		return
	}

	// 4. Send Server Info
	portAttr, err := s.rdmaCtx.QueryPort(s.port)
	if err != nil {
		log.Printf("Failed to query port: %v", err)
		return
	}

	gid, err := s.rdmaCtx.QueryGID(s.port, s.gidIndex)
	if err != nil {
		log.Printf("Failed to query gid: %v", err)
		return
	}

	myInfo := HandshakeInfo{
		QPN:  qp.QPN(),
		LID:  portAttr.LID,
		PSN:  rand.Uint32() & 0xffffff,
		RKey: s.mr.RKey(),
		Addr: s.mr.Addr(),
		GID:  gid,
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(myInfo); err != nil {
		log.Printf("Failed to encode server info: %v", err)
		return
	}

	log.Printf("Server: Received Client Info: QPN=%d, LID=%d, GID=%v", clientInfo.QPN, clientInfo.LID, clientInfo.GID)

	// 5. RTR
	if err := qp.RtrQP(clientInfo.QPN, clientInfo.LID, clientInfo.PSN, clientInfo.GID, s.port, s.gidIndex); err != nil {
		log.Printf("Failed to RTR: %v", err)
		return
	}

	// 6. RTS
	if err := qp.RtsQP(myInfo.PSN); err != nil {
		log.Printf("Failed to RTS: %v", err)
		return
	}

	log.Printf("Connected to client! QP ready.")

	// Post a Recv to catch any Send from client
	if err := qp.PostRecv(s.mr, 0, 1024, 1); err != nil {
		log.Printf("Failed to post recv: %v", err)
		return
	}

	// Poll CQ for completion
	for {
		id, err := cq.PollCQ()
		if err != nil {
			log.Printf("PollCQ failed: %v", err)
			return
		}
		if id > 0 {
			log.Printf("Received message! ID: %d", id)
			// Post another Recv
			if err := qp.PostRecv(s.mr, 0, 1024, 1); err != nil {
				log.Printf("Failed to post recv: %v", err)
				return
			}
		}
		// Busy poll
	}
}
