package twopc

import (
	"distributed-system/util"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"sync"
	"sync/atomic"
	"syscall"
)

// BUG():.

// Participant is the executed role of two-phase commit.
type Participant struct {
	l    net.Listener
	rpcs *rpc.Server
	pool *util.ResourcePool

	dead       int32 // for testing
	unreliable int32 // for testing
	rpcCount   int32 // for testing

	network   string // "unix" or "tcp"
	coord     string // coordinator address
	addr      string // ppt service address
	txnsMu    sync.Mutex
	txnsParts map[string]*TxnPart
	// sbPrepared bool // some particpant has prepared

	callerMap map[string]Caller
}

// RegisterCaller registers a caller with a unique name, which can be used to
// identify the caller in Txn.AddTxnPart and Txn.BroadcastTxnPart.
func (ppt *Participant) RegisterCaller(caller Caller, name string) {
	ppt.callerMap[name] = caller
}

func (ppt *Participant) executeTxnPart(tp *TxnPart) {
	ppt.txnsMu.Lock()
	defer ppt.txnsMu.Unlock()
	caller, ok := ppt.callerMap[tp.CallName]
	if !ok {
		panic("Invalid call: " + tp.CallName)
	}
	tp.errCode, tp.rollbacker = caller.Call(tp.InitRet)
}

// SubmitTxnPart is a RPC call, which submits the TxnPart to the participant
// and executes it. It is returned immediately without waiting for the
// execution of the TxnPart.
//
// The reply could be nil.
func (ppt *Participant) SubmitTxnPart(tp *TxnPart, reply *struct{}) error {
	// TODO
	ppt.txnsMu.Lock()
	tp.state = StateTxnPartWorking
	ppt.txnsParts[tp.ID] = tp
	ppt.txnsMu.Unlock()
	go func() {
		// execute txnpart
		ppt.executeTxnPart(tp)
		if tp.errCode == 0 {
			ppt.txnpartPrepared(tp)
		} else {
			ppt.txnpartAborted(tp)
		}
	}()
	return nil
}

// Prepared is the action when the participant declares
// the prepared state for the part of the transaction.
//
// It will be actively invoked when the business logic
// think the part of the transcation is ok.
func (ppt *Participant) txnpartPrepared(tp *TxnPart) {
	atomic.StoreInt32(&tp.state, StateTxnPartPrepared)
	// assert ppt.me == tp.Shard
	// TODO
	args := &PreparedArgs{
		TxnID:      tp.TxnID,
		TxnPartIdx: tp.Idx,
		ErrCode:    tp.errCode,
	}
	var reply PreparedReply
	ok := false
	for !ok {
		ok = util.RPCPoolCall(ppt.pool, "Coordinator.InformPrepared", args, &reply)
	}

}

// Aborted is the action when the participant aborts
// because of some conditions of business logics.
//
// It should be actively invoked when the business logic
// has to abort the transcation in some conditions. For
// example, the withdraw account doesn't have enough money
// considering transferring money between two accounts.
func (ppt *Participant) txnpartAborted(tp *TxnPart) {
	// TODO
	// this line seems to be unnecessary, could be deleted?
	ppt.updateStateandRB(tp)
	args := AbortedArgs{TxnPartIdx: tp.Idx, TxnID: tp.TxnID, ErrCode: tp.errCode}
	var reply AbortedReply
	var ok = false
	for !ok {
		ok = util.RPCPoolCall(ppt.pool, "Coordinator.InformAborted", args, &reply)
	}
}

func (ppt *Participant) updateStateandRB(tp *TxnPart) {
	atomic.StoreInt32(&tp.state, StateTxnPartAborted)
	ppt.txnsMu.Lock()
	defer ppt.txnsMu.Unlock()
	if tp.canAbort == false {
		tp.canAbort = true
		if tp.rollbacker != nil {
			// has executed
			tp.rollbacker.Rollback()
		}
	}
}

func (ppt *Participant) deleteTxnPart(txnPartID string) *TxnPart {
	ppt.txnsMu.Lock()
	tp := ppt.txnsParts[txnPartID]
	delete(ppt.txnsParts, txnPartID)
	ppt.txnsMu.Unlock()
	return tp
}

// Abort is a RPC call invoked by Coordinator when the coordinator decides
// the transaction should be aborted, including timeout event or receiving
// the Aborted msg from one or more Participants. It could be called not only
// once.
func (ppt *Participant) Abort(args *AbortArgs, reply *AbortReply) error {
	// TODO
	tp := ppt.deleteTxnPart(args.TxnPartID)
	if tp != nil {
		go ppt.updateStateandRB(tp)
	}
	return nil
}

// Commit is a RPC call invoked by Coordinator when the coordinator make sure
// all the participants have entered the Prepared state. It could be called
// not only once.
func (ppt *Participant) Commit(args *CommitArgs, reply *CommitReply) error {
	// TODO
	tp := ppt.deleteTxnPart(args.TxnPartID)
	if tp != nil {
		atomic.StoreInt32(&tp.state, StateTxnPartCommitted)
	}
	return nil
}

// DefaultPptPoolSize is the maximum number of connections in the pool from the
// Participant to the Coordinator.
const DefaultPptPoolSize = 5

// NewParticipant init a participant service. Network could be "tcp" or "unix".
// Coord is the listened address on the Coordiantor. Addr is the listened
// address of this Participant.
func NewParticipant(network, addr, coord string) *Participant {
	ppt := &Participant{network: network, addr: addr, coord: coord,
		txnsParts: make(map[string]*TxnPart), callerMap: make(map[string]Caller)}
	ppt.pool = util.NewResourcePool(func() util.Resource {
		return util.DialServer(network, coord)
	}, DefaultPptPoolSize)

	l, e := net.Listen(network, addr)
	if e != nil {
		log.Printf("listen error: %v\n", e)
		return nil
	}
	log.Printf("listen successfully @%v\n", addr)
	ppt.l = l
	rpcs := rpc.NewServer()
	rpcs.Register(ppt)
	ppt.rpcs = rpcs

	// Don't change any of the following code,
	// or do anything to subvert it.

	// Create a thread to accept RPC connections
	go func() {
		for ppt.isdead() == false {
			conn, err := ppt.l.Accept()
			if err == nil && ppt.isdead() == false {
				if ppt.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if ppt.isunreliable() && (rand.Int63()%1000) < 200 {
					// process the request but force discard of reply.
					c1 := conn.(*net.UnixConn)
					f, _ := c1.File()
					err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
					if err != nil {
						fmt.Printf("shutdown: %v\n", err)
					}
					atomic.AddInt32(&ppt.rpcCount, 1)
					go rpcs.ServeConn(conn)
				} else {
					atomic.AddInt32(&ppt.rpcCount, 1)
					go rpcs.ServeConn(conn)
				}
			} else if err == nil {
				conn.Close()
			}
			if err != nil && ppt.isdead() == false {
				fmt.Printf("Participant(%v) accept: %v\n", ppt.addr, err.Error())
			}
		}
	}()
	return ppt
}

// RegisterRPCService registers the service onto the rpc calls.
func (ppt *Participant) RegisterRPCService(service interface{}) {
	ppt.rpcs.Register(service)
}

// Kill tell the peer to shut itself down.
// for testing.
// please do not change these two functions.
func (ppt *Participant) Kill() {
	atomic.StoreInt32(&ppt.dead, 1)
	if ppt.l != nil {
		ppt.l.Close()
	}
}

// Has this peer been asked to shut down?
func (ppt *Participant) isdead() bool {
	return atomic.LoadInt32(&ppt.dead) != 0
}

// Please do not change these two functions.
func (ppt *Participant) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&ppt.unreliable, 1)
	} else {
		atomic.StoreInt32(&ppt.unreliable, 0)
	}
}

func (ppt *Participant) isunreliable() bool {
	return atomic.LoadInt32(&ppt.unreliable) != 0
}
