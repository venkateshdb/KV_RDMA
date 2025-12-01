package rdma

/*
#cgo LDFLAGS: -libverbs
#include <infiniband/verbs.h>
#include <stdlib.h>
#include <poll.h>
#include <errno.h>

// Helper to post RDMA Write
int post_write(struct ibv_qp *qp, struct ibv_mr *mr, void *addr, uint32_t length, uint64_t remote_addr, uint32_t rkey, uint64_t wr_id, int flags) {
    struct ibv_sge sge;
    sge.addr = (uint64_t)addr;
    sge.length = length;
    sge.lkey = mr->lkey;

    struct ibv_send_wr wr;
    wr.wr_id = wr_id;
    wr.next = NULL;
    wr.sg_list = &sge;
    wr.num_sge = 1;
    wr.opcode = IBV_WR_RDMA_WRITE;
    wr.send_flags = flags;
    wr.wr.rdma.remote_addr = remote_addr;
    wr.wr.rdma.rkey = rkey;

    struct ibv_send_wr *bad_wr;
    return ibv_post_send(qp, &wr, &bad_wr);
}

// Helper to post Send
int post_send(struct ibv_qp *qp, struct ibv_mr *mr, void *addr, uint32_t length, uint64_t wr_id, int flags) {
    struct ibv_sge sge;
    sge.addr = (uint64_t)addr;
    sge.length = length;
    sge.lkey = mr->lkey;

    struct ibv_send_wr wr;
    wr.wr_id = wr_id;
    wr.next = NULL;
    wr.sg_list = &sge;
    wr.num_sge = 1;
    wr.opcode = IBV_WR_SEND;
    wr.send_flags = flags;

    struct ibv_send_wr *bad_wr;
    return ibv_post_send(qp, &wr, &bad_wr);
}

// Helper to post Recv
int post_recv(struct ibv_qp *qp, struct ibv_mr *mr, void *addr, uint32_t length, uint64_t wr_id) {
    struct ibv_sge sge;
    sge.addr = (uint64_t)addr;
    sge.length = length;
    sge.lkey = mr->lkey;

    struct ibv_recv_wr wr;
    wr.wr_id = wr_id;
    wr.next = NULL;
    wr.sg_list = &sge;
    wr.num_sge = 1;

    struct ibv_recv_wr *bad_wr;
    return ibv_post_recv(qp, &wr, &bad_wr);
}

// Helper to post RDMA Read
int post_read(struct ibv_qp *qp, struct ibv_mr *mr, void *addr, uint32_t length, uint64_t remote_addr, uint32_t rkey, uint64_t wr_id, int flags) {
    struct ibv_sge sge;
    sge.addr = (uint64_t)addr;
    sge.length = length;
    sge.lkey = mr->lkey;

    struct ibv_send_wr wr;
    wr.wr_id = wr_id;
    wr.next = NULL;
    wr.sg_list = &sge;
    wr.num_sge = 1;
    wr.opcode = IBV_WR_RDMA_READ;
    wr.send_flags = flags;
    wr.wr.rdma.remote_addr = remote_addr;
    wr.wr.rdma.rkey = rkey;

    struct ibv_send_wr *bad_wr;
    return ibv_post_send(qp, &wr, &bad_wr);
}

// Helper to query port
int query_port(struct ibv_context *ctx, uint8_t port_num, struct ibv_port_attr *attr) {
    return ibv_query_port(ctx, port_num, attr);
}

// Helper to query GID
int query_gid(struct ibv_context *ctx, uint8_t port_num, int index, union ibv_gid *gid) {
    return ibv_query_gid(ctx, port_num, index, gid);
}
struct batch_write_item {
    uint64_t addr;
    uint32_t length;
    uint64_t remote_addr;
    uint32_t rkey;
    uint64_t wr_id;
};

int post_write_batch(struct ibv_qp *qp, struct ibv_mr *mr, struct batch_write_item *items, int count, int flags) {
    // We need to allocate arrays for sges and wrs.
    // Since C99 VLAs are not always available or safe for large stacks, we use malloc.
    // However, for performance, we want to avoid malloc.
    // Let's assume a max batch size or use a fixed size buffer on stack if small, else malloc.
    // For now, let's use malloc for simplicity, or better, just iterate and link.

    // Actually, we can't easily allocate variable size arrays on stack in standard C without VLAs.
    // But we can allocate them on the heap.

    struct ibv_sge *sges = malloc(count * sizeof(struct ibv_sge));
    struct ibv_send_wr *wrs = malloc(count * sizeof(struct ibv_send_wr));
    if (!sges || !wrs) {
        free(sges);
        free(wrs);
        return -1;
    }

    struct ibv_send_wr *bad_wr;

    for (int i = 0; i < count; i++) {
        sges[i].addr = items[i].addr;
        sges[i].length = items[i].length;
        sges[i].lkey = mr->lkey;

        wrs[i].wr_id = items[i].wr_id;
        wrs[i].next = (i < count - 1) ? &wrs[i+1] : NULL;
        wrs[i].sg_list = &sges[i];
        wrs[i].num_sge = 1;
        wrs[i].opcode = IBV_WR_RDMA_WRITE;
        wrs[i].send_flags = (i == count - 1) ? flags : 0; // Only signal the last one
        wrs[i].wr.rdma.remote_addr = items[i].remote_addr;
        wrs[i].wr.rdma.rkey = items[i].rkey;
    }

    int ret = ibv_post_send(qp, &wrs[0], &bad_wr);

    free(sges);
    free(wrs);
    return ret;
}
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

// Constants for flags
const (
	IBV_SEND_SIGNALED = C.IBV_SEND_SIGNALED
)

// Context represents an RDMA context (device + protection domain)
type Context struct {
	ibvCtx *C.struct_ibv_context
	pd     *C.struct_ibv_pd
}

// NewContext opens the specified RDMA device and allocates a Protection Domain.
// If deviceName is empty, it uses the first available device.
func NewContext(deviceName string) (*Context, error) {
	var numDevices C.int
	deviceList := C.ibv_get_device_list(&numDevices)
	if deviceList == nil {
		return nil, errors.New("failed to get RDMA device list")
	}
	defer C.ibv_free_device_list(deviceList)

	if numDevices == 0 {
		return nil, errors.New("no RDMA devices found")
	}

	var device *C.struct_ibv_device
	if deviceName == "" {
		device = *deviceList // Use the first one
	} else {
		devices := (*[1 << 30]*C.struct_ibv_device)(unsafe.Pointer(deviceList))[:numDevices:numDevices]
		for _, d := range devices {
			name := C.GoString(C.ibv_get_device_name(d))
			if name == deviceName {
				device = d
				break
			}
		}
		if device == nil {
			return nil, fmt.Errorf("device %s not found", deviceName)
		}
	}

	ibvCtx := C.ibv_open_device(device)
	if ibvCtx == nil {
		return nil, errors.New("failed to open RDMA device")
	}

	pd := C.ibv_alloc_pd(ibvCtx)
	if pd == nil {
		C.ibv_close_device(ibvCtx)
		return nil, errors.New("failed to allocate protection domain")
	}

	return &Context{
		ibvCtx: ibvCtx,
		pd:     pd,
	}, nil
}

// QueryGID queries the GID at the specified index.
func (c *Context) QueryGID(portNum int, index int) ([]byte, error) {
	var gid C.union_ibv_gid
	if C.query_gid(c.ibvCtx, C.uint8_t(portNum), C.int(index), &gid) != 0 {
		return nil, errors.New("failed to query gid")
	}
	g := *(*[16]byte)(unsafe.Pointer(&gid))
	return g[:], nil
}

// PortAttr wraps ibv_port_attr
type PortAttr struct {
	LID uint16
}

// QueryPort queries port attributes.
func (c *Context) QueryPort(portNum int) (*PortAttr, error) {
	var attr C.struct_ibv_port_attr
	if C.query_port(c.ibvCtx, C.uint8_t(portNum), &attr) != 0 {
		return nil, errors.New("failed to query port")
	}
	return &PortAttr{LID: uint16(attr.lid)}, nil
}

// MemoryRegion wraps ibv_mr
type MemoryRegion struct {
	mr *C.struct_ibv_mr
}

// RegisterMR registers a memory region.
// accessFlags can be bitwise OR of:
// C.IBV_ACCESS_LOCAL_WRITE | C.IBV_ACCESS_REMOTE_WRITE | C.IBV_ACCESS_REMOTE_READ
func (c *Context) RegisterMR(ptr unsafe.Pointer, size int, accessFlags int) (*MemoryRegion, error) {
	mr := C.ibv_reg_mr(c.pd, ptr, C.size_t(size), C.int(accessFlags))
	if mr == nil {
		return nil, errors.New("failed to register memory region")
	}
	return &MemoryRegion{mr: mr}, nil
}

// LKey returns the local key.
func (mr *MemoryRegion) LKey() uint32 {
	return uint32(mr.mr.lkey)
}

// RKey returns the remote key.
func (mr *MemoryRegion) RKey() uint32 {
	return uint32(mr.mr.rkey)
}

// Addr returns the address.
func (mr *MemoryRegion) Addr() uint64 {
	return uint64(uintptr(mr.mr.addr))
}

// Deregister deregisters the memory region.
func (mr *MemoryRegion) Deregister() {
	if mr.mr != nil {
		C.ibv_dereg_mr(mr.mr)
	}
}

// QueuePair wraps ibv_qp
type QueuePair struct {
	qp *C.struct_ibv_qp
}

// CreateQP creates a Queue Pair.
func (c *Context) CreateQP(cq *CompletionQueue) (*QueuePair, error) {
	var attr C.struct_ibv_qp_init_attr

	attr.send_cq = cq.cq
	attr.recv_cq = cq.cq
	attr.cap.max_send_wr = 128
	attr.cap.max_recv_wr = 128
	attr.cap.max_send_sge = 1
	attr.cap.max_recv_sge = 1
	attr.qp_type = C.IBV_QPT_RC // Reliable Connected

	qp := C.ibv_create_qp(c.pd, &attr)
	if qp == nil {
		return nil, errors.New("failed to create QP")
	}
	return &QueuePair{qp: qp}, nil
}

// QPN returns the Queue Pair Number.
func (qp *QueuePair) QPN() uint32 {
	return uint32(qp.qp.qp_num)
}

// CompletionQueue wraps ibv_cq
type CompletionQueue struct {
	cq *C.struct_ibv_cq
}

// CreateCQ creates a Completion Queue.
// If cc is not nil, the CQ will be associated with the completion channel.
// CreateCQ creates a Completion Queue.
func (c *Context) CreateCQ(cqe int) (*CompletionQueue, error) {
	cq := C.ibv_create_cq(c.ibvCtx, C.int(cqe), nil, nil, 0)
	if cq == nil {
		return nil, errors.New("failed to create CQ")
	}
	return &CompletionQueue{cq: cq}, nil
}

// Destroy destroys the QP.
func (qp *QueuePair) Destroy() {
	if qp.qp != nil {
		C.ibv_destroy_qp(qp.qp)
	}
}

// Destroy destroys the CQ.
func (cq *CompletionQueue) Destroy() {
	if cq.cq != nil {
		C.ibv_destroy_cq(cq.cq)
	}
}

// PollCQ polls the completion queue.
// Returns (wrID, nil) on success, (0, nil) if empty, (0, error) on failure.
func (cq *CompletionQueue) PollCQ() (uint64, error) {
	var wc C.struct_ibv_wc
	n := C.ibv_poll_cq(cq.cq, 1, &wc)
	if n < 0 {
		return 0, errors.New("poll cq failed")
	}
	if n == 0 {
		return 0, nil
	}
	if wc.status != C.IBV_WC_SUCCESS {
		return 0, fmt.Errorf("wc status error: %d", wc.status)
	}
	return uint64(wc.wr_id), nil
}

// Close releases resources.
func (c *Context) Close() {
	if c.pd != nil {
		C.ibv_dealloc_pd(c.pd)
	}
	if c.ibvCtx != nil {
		C.ibv_close_device(c.ibvCtx)
	}
}

// InitQP transitions QP to INIT state.
func (qp *QueuePair) InitQP(portNum int) error {
	var attr C.struct_ibv_qp_attr
	attr.qp_state = C.IBV_QPS_INIT
	attr.pkey_index = 0
	attr.port_num = C.uint8_t(portNum)
	attr.qp_access_flags = C.IBV_ACCESS_LOCAL_WRITE | C.IBV_ACCESS_REMOTE_READ | C.IBV_ACCESS_REMOTE_WRITE

	mask := C.IBV_QP_STATE | C.IBV_QP_PKEY_INDEX | C.IBV_QP_PORT | C.IBV_QP_ACCESS_FLAGS

	ret := C.ibv_modify_qp(qp.qp, &attr, C.int(mask))
	if ret != 0 {
		return fmt.Errorf("failed to modify QP to INIT: %d", ret)
	}
	return nil
}

// RtrQP transitions QP to RTR state.
// For RoCE, we need remoteGID. If remoteGID is nil, we assume IB (LID-based).
func (qp *QueuePair) RtrQP(remoteQPN uint32, remoteLID uint16, remotePSN uint32, remoteGID []byte, portNum, gidIndex int) error {
	var attr C.struct_ibv_qp_attr
	attr.qp_state = C.IBV_QPS_RTR
	attr.path_mtu = C.IBV_MTU_1024
	attr.dest_qp_num = C.uint32_t(remoteQPN)
	attr.rq_psn = C.uint32_t(remotePSN)
	attr.max_dest_rd_atomic = 1
	attr.min_rnr_timer = 12

	attr.ah_attr.dlid = C.uint16_t(remoteLID)
	attr.ah_attr.sl = 0
	attr.ah_attr.src_path_bits = 0
	attr.ah_attr.port_num = C.uint8_t(portNum)

	if len(remoteGID) == 16 {
		attr.ah_attr.is_global = 1
		attr.ah_attr.grh.dgid = *(*C.union_ibv_gid)(unsafe.Pointer(&remoteGID[0]))
		attr.ah_attr.grh.sgid_index = C.uint8_t(gidIndex)
		attr.ah_attr.grh.hop_limit = 0
		attr.ah_attr.grh.traffic_class = 0
	} else {
		attr.ah_attr.is_global = 0
	}

	mask := C.IBV_QP_STATE | C.IBV_QP_AV | C.IBV_QP_PATH_MTU | C.IBV_QP_DEST_QPN | C.IBV_QP_RQ_PSN | C.IBV_QP_MAX_DEST_RD_ATOMIC | C.IBV_QP_MIN_RNR_TIMER

	ret := C.ibv_modify_qp(qp.qp, &attr, C.int(mask))
	if ret != 0 {
		return fmt.Errorf("failed to modify QP to RTR: %d", ret)
	}
	return nil
}

// RtsQP transitions QP to RTS state.
func (qp *QueuePair) RtsQP(myPSN uint32) error {
	var attr C.struct_ibv_qp_attr
	attr.qp_state = C.IBV_QPS_RTS
	attr.timeout = 14
	attr.retry_cnt = 7
	attr.rnr_retry = 7
	attr.sq_psn = C.uint32_t(myPSN)
	attr.max_rd_atomic = 1

	mask := C.IBV_QP_STATE | C.IBV_QP_TIMEOUT | C.IBV_QP_RETRY_CNT | C.IBV_QP_RNR_RETRY | C.IBV_QP_SQ_PSN | C.IBV_QP_MAX_QP_RD_ATOMIC

	ret := C.ibv_modify_qp(qp.qp, &attr, C.int(mask))
	if ret != 0 {
		return fmt.Errorf("failed to modify QP to RTS: %d", ret)
	}
	return nil
}

// PostWrite posts an RDMA Write operation.
func (qp *QueuePair) PostWrite(mr *MemoryRegion, offset, length int, remoteAddr uint64, rkey uint32, wrID uint64, flags int) error {
	addr := unsafe.Pointer(uintptr(mr.mr.addr) + uintptr(offset))
	ret := C.post_write(qp.qp, mr.mr, addr, C.uint32_t(length), C.uint64_t(remoteAddr), C.uint32_t(rkey), C.uint64_t(wrID), C.int(flags))
	if ret != 0 {
		return fmt.Errorf("ibv_post_send failed: %d", ret)
	}
	return nil
}

// PostSend posts a Send operation.
func (qp *QueuePair) PostSend(mr *MemoryRegion, offset, length int, wrID uint64, flags int) error {
	addr := unsafe.Pointer(uintptr(mr.mr.addr) + uintptr(offset))
	ret := C.post_send(qp.qp, mr.mr, addr, C.uint32_t(length), C.uint64_t(wrID), C.int(flags))
	if ret != 0 {
		return fmt.Errorf("ibv_post_send failed: %d", ret)
	}
	return nil
}

// PostRecv posts a Recv operation.
func (qp *QueuePair) PostRecv(mr *MemoryRegion, offset, length int, wrID uint64) error {
	addr := unsafe.Pointer(uintptr(mr.mr.addr) + uintptr(offset))
	ret := C.post_recv(qp.qp, mr.mr, addr, C.uint32_t(length), C.uint64_t(wrID))
	if ret != 0 {
		return fmt.Errorf("ibv_post_recv failed: %d", ret)
	}
	return nil
}

// PostRead posts a RDMA Read request.
func (qp *QueuePair) PostRead(mr *MemoryRegion, offset, length int, remoteAddr uint64, rkey uint32, wrID uint64, flags int) error {
	addr := unsafe.Pointer(uintptr(mr.mr.addr) + uintptr(offset))
	ret := C.post_read(qp.qp, mr.mr, addr, C.uint32_t(length), C.uint64_t(remoteAddr), C.uint32_t(rkey), C.uint64_t(wrID), C.int(flags))
	if ret != 0 {
		return fmt.Errorf("failed to post read: %d", ret)
	}
	return nil
}

// WriteItem represents a single write operation in a batch.
type WriteItem struct {
	Offset     int
	Length     int
	RemoteAddr uint64
	RKey       uint32
	WrID       uint64
}

// PostWriteBatch posts a batch of RDMA Write operations.
// Only the last operation is signaled.
func (qp *QueuePair) PostWriteBatch(mr *MemoryRegion, items []WriteItem, flags int) error {
	if len(items) == 0 {
		return nil
	}

	cItems := make([]C.struct_batch_write_item, len(items))
	for i, item := range items {
		cItems[i].addr = C.uint64_t(uintptr(mr.mr.addr) + uintptr(item.Offset))
		cItems[i].length = C.uint32_t(item.Length)
		cItems[i].remote_addr = C.uint64_t(item.RemoteAddr)
		cItems[i].rkey = C.uint32_t(item.RKey)
		cItems[i].wr_id = C.uint64_t(item.WrID)
	}

	ret := C.post_write_batch(qp.qp, mr.mr, &cItems[0], C.int(len(items)), C.int(flags))
	if ret != 0 {
		return fmt.Errorf("ibv_post_send batch failed: %d", ret)
	}
	return nil
}
