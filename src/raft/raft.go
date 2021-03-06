package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import "labrpc"
import "time"
import "math/rand"
import "bytes"
import "encoding/gob"

//the struct for log
type Log struct {
	Command interface{}
	Index   int
	Term    int
}

//the role for raft
const (
	FOLLOWER  = "follower"
	CANDIDATE = "candidate"
	LEADER    = "leader"
)

//the heartbeat time and timemout time
const (
	HeartBeatTime   = time.Millisecond * 50
	ElectionMinTime = 150
	ElectionMaxTime = 300
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Term        int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[](should be persisted)
	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	VotedFor    int   //have voted for which candidate in this term(should be persisted)
	CurrentTerm int   //(should be persisted)
	Logs        []Log //(should be persisted)
	//volatile state for all servers
	commitIndex int
	lastApplied int
	//volatile state for leader
	nextIndex  []int
	matchIndex []int
	beVoted    int

	lastIncludeIndex int
	lastIncludeTerm  int
	state            string
	applyCh          chan ApplyMsg
	timer            *time.Timer
}

type InstallSnapshotArgs struct {
	Term             int
	LeaderId         int
	LastIncludeIndex int
	LastIncludeTerm  int
	Data             []byte
}

type InstallSnapshotReply struct {
	Term int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.CurrentTerm
	isleader = (rf.state == LEADER)
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(rf.VotedFor)
	e.Encode(rf.CurrentTerm)
	e.Encode(rf.Logs)
	e.Encode(rf.lastIncludeIndex)
	e.Encode(rf.lastIncludeTerm)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)
	d.Decode(&rf.VotedFor)
	d.Decode(&rf.CurrentTerm)
	d.Decode(&rf.Logs)
	d.Decode(&rf.lastIncludeIndex)
	d.Decode(&rf.lastIncludeTerm)
}

func (rf *Raft) RaftStateSize() int {
	return rf.persister.RaftStateSize()
}

func (rf *Raft) ReadSnapshot() []byte {
	return rf.persister.ReadSnapshot()
}

func (rf *Raft) SaveSnapshot(data []byte, index int) {
	DPrintf("enter Raft Savesnap, the rf.mu: %v", rf.mu)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("Raft SaveSnapshot becalled")
	pos, ok := rf.getLogPos(index)
	if ok {
		rf.lastIncludeIndex = index
		rf.lastIncludeTerm = rf.Logs[pos].Term
		rf.Logs = rf.Logs[pos:] //the log at pos will not be discard
		rf.persist()
		rf.persister.SaveSnapshotAndRaftState(data, rf.persister.ReadRaftState())
	}
}

func (rf *Raft) getLogPos(index int) (int, bool) {
	ok := false
	n := len(rf.Logs)
	pos := -1
	if n > 0 && index >= rf.Logs[0].Index && rf.Logs[n-1].Index >= index {
		pos = index - rf.Logs[0].Index
		ok = true
	}
	return pos, ok
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

//AppendEntryArgs, for heartbeats and logs
type AppendEntryArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []Log
	LeaderCommit int
}

//the reply for AppendEnty RPC
type AppendEntryReply struct {
	Term         int
	Success      bool
	ConfirmIndex int
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//fmt.Println("I am", rf.me, "i get a vote request:", args, "my term is:", rf.CurrentTerm)
	willVote := true
	willReset := false

	n := len(rf.Logs)
	if n > 0 { // candidate's logs is older than this raft
		if rf.Logs[n-1].Term > args.LastLogTerm ||
			(rf.Logs[n-1].Term == args.LastLogTerm && rf.Logs[n-1].Index > args.LastLogIndex) {
			willVote = false
		}
	}

	if args.Term < rf.CurrentTerm { //candidate's term is out of date
		willVote = false
	} else if args.Term > rf.CurrentTerm {
		rf.CurrentTerm = args.Term
		rf.state = FOLLOWER
		rf.VotedFor = -1
		willReset = true
		rf.persist()
	} else if args.Term == rf.CurrentTerm && rf.VotedFor != -1 { //if it has voted for itself
		willVote = false
	}

	reply.Term = rf.CurrentTerm
	reply.VoteGranted = willVote

	if willVote == true {
		rf.VotedFor = args.CandidateId
		rf.state = FOLLOWER
		rf.persist()
		//willReset = true
	}
	//fmt.Println("I am", rf.me, "i get a vote request:", args, "my ans is:", reply)
	if willReset == true {
		rf.resetTimer()
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	//fmt.Println("I am", rf.me, "sending vote request to", server, "args:", args)
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

//append entry RPC handler
func (rf *Raft) AppendEntries(args *AppendEntryArgs, reply *AppendEntryReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.CurrentTerm {
		reply.Success = false
		reply.Term = rf.CurrentTerm
	} else {
		rf.state = FOLLOWER
		rf.CurrentTerm = args.Term
		reply.Term = args.Term
		// Since at first, leader communicates with followers,
		// nextIndex[server] value equal to max index of leader
		// so system need to find the matching term and index
		n := len(rf.Logs)
		prevIndexPos, ok := rf.getLogPos(args.PrevLogIndex)
		if n > 0 && (args.PrevLogIndex-rf.Logs[0].Index) < 0 {
			reply.ConfirmIndex = rf.Logs[0].Index
			reply.Success = false
		} else if (n > 0 && !ok) ||
			(ok && args.PrevLogTerm != rf.Logs[prevIndexPos].Term) {
			// rf.logger.Printf("Match failed %v\n", args)
			reply.ConfirmIndex = rf.Logs[n-1].Index
			if reply.ConfirmIndex > args.PrevLogIndex {
				reply.ConfirmIndex = args.PrevLogIndex
			}
			for reply.ConfirmIndex > rf.commitIndex && reply.ConfirmIndex > 0 {
				pos, ok := rf.getLogPos(reply.ConfirmIndex)
				if ok && rf.Logs[pos].Term == args.PrevLogTerm {
					break
				}
				reply.ConfirmIndex--
			}
			reply.Success = false
		} else if n == 0 && args.PrevLogIndex != 0 {
			reply.ConfirmIndex = 0
			reply.Success = false
		} else if args.Entries != nil {
			//fmt.Println("I am ", rf.me, "I get appd: ", args.Entries)
			if n > 0 {
				pos, _ := rf.getLogPos(args.PrevLogIndex)
				rf.Logs = rf.Logs[:pos+1]
				//fmt.Println("I am ", rf.me, "I get cut my logs and get: ", args.Entries)
			}
			rf.Logs = append(rf.Logs, args.Entries...)
			m := len(rf.Logs)
			if m > 0 && rf.Logs[m-1].Index >= args.LeaderCommit {
				rf.commitIndex = args.LeaderCommit
				go rf.commitLogs()
			}
			if m > 0 {
				reply.ConfirmIndex = rf.Logs[m-1].Index
			} else {
				reply.ConfirmIndex = 0
			}
			reply.Success = true
		} else {
			if n > 0 && rf.Logs[n-1].Index >= args.LeaderCommit {
				rf.commitIndex = args.LeaderCommit
				go rf.commitLogs()
			}
			reply.ConfirmIndex = args.PrevLogIndex
			reply.Success = true
		}
	}
	reply.ConfirmIndex++
	rf.persist()
	rf.resetTimer()
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	//todo
	rf.mu.Lock()
	DPrintf("I am %v, my log:%v, recieve snapshot: %v", rf.me, rf.Logs, args)
	reply.Term = rf.CurrentTerm
	if args.Term < rf.CurrentTerm {
		DPrintf("I am %v, my term: %v, snapshot term: %v, so don't install shnapshot and return!")
		rf.mu.Unlock()
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.CurrentTerm = args.LastIncludeTerm
		rf.state = FOLLOWER
		rf.VotedFor = -1
	}
	pos, ok := rf.getLogPos(args.LastIncludeIndex)
	if !ok {
		//to make sure there is at least one log except just start
		firstLog := Log{Command: 0,
			Index: args.LastIncludeIndex,
			Term:  args.LastIncludeTerm,
		}
		rf.Logs = make([]Log, 0)
		rf.Logs = append(rf.Logs, firstLog)
	} else {
		rf.Logs = rf.Logs[pos:]
	}
	DPrintf("after reshape, my log :%v", rf.Logs)
	rf.persist()
	rf.persister.SaveSnapshotAndRaftState(args.Data, rf.persister.ReadRaftState())
	rf.mu.Unlock()
	msg := ApplyMsg{UseSnapshot: true,
		Snapshot: args.Data}
	DPrintf("send snapshot msg to applyChan %v: ", msg)
	rf.applyCh <- msg
	DPrintf("have send snapshot msg to applyChan %v: ", msg)
}

func (rf *Raft) handleInstallSnapshotReply(server int, includeIndex int, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if reply.Term > rf.CurrentTerm {
		rf.state = FOLLOWER
		rf.VotedFor = -1
		rf.persist()
		rf.resetTimer()
		return
	}
	rf.nextIndex[server] = includeIndex + 1
	rf.matchIndex[server] = includeIndex
}

func minInt(x int, y int) int {
	if x > y {
		return y
	} else {
		return x
	}
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntryArgs, reply *AppendEntryReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

func (rf *Raft) handleAppendEntriesReply(server int, reply *AppendEntryReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state != LEADER {
		return
	}

	if reply.Term > rf.CurrentTerm {
		rf.CurrentTerm = reply.Term
		rf.state = FOLLOWER
		rf.VotedFor = -1
		rf.persist()
		rf.resetTimer()
		return
	}

	//fmt.Println("I am", rf.me, "and get a reply:", reply)
	if reply.Success == true {
		n := len(rf.Logs)
		if n == 0 {
			rf.nextIndex[server] = 1
		} else {
			rf.nextIndex[server] = reply.ConfirmIndex
		}
		rf.matchIndex[server] = rf.nextIndex[server] - 1

		majorCount := 0
		for i := 0; i < len(rf.matchIndex); i++ {
			if rf.matchIndex[i] >= rf.matchIndex[server] && rf.matchIndex[i] != 0 {
				if i == rf.me {
					continue
				}
				majorCount++
			}
		}

		if majorCount >= len(rf.peers)/2 && rf.commitIndex < rf.matchIndex[server] {
			pos, _ := rf.getLogPos(rf.matchIndex[server])
			if rf.CurrentTerm == rf.Logs[pos].Term {
				rf.commitIndex = rf.matchIndex[server]
				go rf.commitLogs()
			}
		}
	} else {
		//fmt.Println("shit! reply for", server," append false!, dec and try another time!")
		rf.nextIndex[server] = reply.ConfirmIndex
		DPrintf("shit, server %v reply false!, update next[%v]:%v", server, server, reply.ConfirmIndex)
		rf.appendToFollowers()
		//rf.appendToFollower(server)
	}
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	DPrintf("rf %v Enter start, command:%v", rf.me, command)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DPrintf("rf %v be called start, command:%v", rf.me, command)
	index := -1
	term := -1
	isLeader := false
	// Your code here (2B).
	if rf.state == LEADER {
		n := len(rf.Logs)
		var newIndex int

		if n > 0 {
			newIndex = rf.Logs[n-1].Index + 1
		} else {
			newIndex = 1
		}

		newLog := Log{
			Command: command,
			Index:   newIndex,
			Term:    rf.CurrentTerm,
		}

		rf.Logs = append(rf.Logs, newLog)
		rf.persist()
		index = newIndex
		term = rf.CurrentTerm
		isLeader = true
		DPrintf("raft %v is leader, and append new log %v", rf.me, newLog)
	}

	return index, term, isLeader
}

//commit index to state machine, must be run as a new goroutline
func (rf *Raft) commitLogs() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//fmt.Println("I'm:", rf.me, "role:", rf.state, "start to commitlog, logs:", rf.Logs)
	n := len(rf.Logs)
	if n == 0 {
		return
	}

	if rf.lastApplied < rf.commitIndex {
		startPos := 0
		if rf.lastApplied > 0 {
			startPos, _ = rf.getLogPos(rf.lastApplied)
			startPos++
		}

		endPos, _ := rf.getLogPos(rf.commitIndex)
		//fmt.Println("oh!we can start to commit, pos range:", startPos, endPos)
		for ; startPos <= endPos; startPos++ {
			msg := ApplyMsg{
				Index:       rf.Logs[startPos].Index,
				Term:        rf.Logs[startPos].Term,
				Command:     rf.Logs[startPos].Command,
				UseSnapshot: false,
				Snapshot:    []byte{},
			}
			//fmt.Println("to apply index:", msg.Index)
			DPrintf("rf %v apply index %v", rf.me, msg)
			rf.applyCh <- msg
			//fmt.Println("have applied index:", msg.Index)
			DPrintf("rf %v have applied index %v", rf.me, msg)
			rf.lastApplied = msg.Index
		}
	}
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}

func (rf *Raft) handleVoteReply(reply_args *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state == CANDIDATE && reply_args.VoteGranted == true {
		rf.beVoted++
		if rf.beVoted > len(rf.peers)/2 {
			rf.state = LEADER
			rf.resetTimer()
			//send heartbeat to others
			//rf.appendToFollowers()
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					continue
				}
				rf.matchIndex[i] = 0
				if len(rf.Logs) > 0 {
					rf.nextIndex[i] = rf.Logs[len(rf.Logs)-1].Index + 1
				} else {
					rf.nextIndex[i] = 1
				}
			}
		}
	} else if reply_args.VoteGranted == false && reply_args.Term > rf.CurrentTerm {
		rf.state = FOLLOWER
		rf.CurrentTerm = reply_args.Term
		rf.persist()
		rf.VotedFor = -1
		rf.resetTimer()
	}
}

func (rf *Raft) handleTimer() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//fmt.Println("raft:", rf.me, "timeout! state:", rf.state, "logs:", rf.Logs, "term:", rf.CurrentTerm)
	if rf.state == LEADER {
		//fmt.Println("leader heartbeat, show raft:", rf.me, "log state:", rf.Logs)
		rf.appendToFollowers()
	} else { // start a leader election

		rf.state = CANDIDATE
		rf.beVoted = 1
		rf.VotedFor = rf.me
		rf.CurrentTerm++
		rf.persist()

		args := RequestVoteArgs{
			Term:         rf.CurrentTerm,
			CandidateId:  rf.me,
			LastLogIndex: 0,
			LastLogTerm:  0,
		}

		log_num := len(rf.Logs)
		if log_num > 0 {
			args.LastLogIndex = rf.Logs[log_num-1].Index
			args.LastLogTerm = rf.Logs[log_num-1].Term
		}

		for i := 0; i < len(rf.peers); i++ {
			if i == rf.me {
				continue
			}

			go func(sever int, args RequestVoteArgs) {
				reply_args := RequestVoteReply{}
				//fmt.Println("I am", rf.me, rf.state, "I am sending Request vote !", args)
				ok := rf.sendRequestVote(sever, &args, &reply_args)
				if ok != false {
					rf.handleVoteReply(&reply_args)
				}
			}(i, args)
		}
	}
	rf.resetTimer()
}

/*
func findLogByIndex(logs []Log, index int) (pos int, ok bool) {
	if len(logs) <= 0 {
		return -1, false
	}

	pos = -1
	ok = false
	for n := len(logs) - 1; n >= 0; n-- {
		if logs[n].Index == index {
			pos = n
			ok = true
			break
		} else if logs[n].Index < index {
			break
		}
	}

	return pos, ok
}
*/

//向其他raft节点发送Append Entries
func (rf *Raft) appendToFollowers() {

	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		rf.appendToFollower(i)
	}
}

func (rf *Raft) appendToFollower(sever int) {
	i := sever
	next := rf.nextIndex[i]
	//fmt.Println("I am ", rf.me, " send append to ", sever)
	//fmt.Println("my log: ", rf.Logs)
	//fmt.Println("have index to send!!it's:", next)
	n := len(rf.Logs)
	DPrintf("I am %v, start to append to %v", rf.me, sever)
	if n > 0 && rf.lastIncludeIndex == rf.Logs[0].Index && rf.Logs[0].Index >= next {
		args := InstallSnapshotArgs{
			Term:             rf.CurrentTerm,
			LeaderId:         rf.me,
			LastIncludeIndex: rf.Logs[0].Index,
			LastIncludeTerm:  rf.Logs[0].Term,
			Data:             rf.persister.ReadSnapshot(),
		}
		DPrintf("I am %v, send snapshot to %v, snapshot: %v", rf.me, sever, args)
		go func(server int, args InstallSnapshotArgs) {
			reply := InstallSnapshotReply{}
			ok := rf.sendInstallSnapshot(server, &args, &reply)
			DPrintf("call recieve snapshot OK, ready to handle reply")
			if ok {
				rf.handleInstallSnapshotReply(server, args.LastIncludeIndex, &reply)
			}
		}(sever, args)
	} else {
		args := AppendEntryArgs{
			Term:         rf.CurrentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: 0,
			PrevLogTerm:  0,
			Entries:      []Log{},
			LeaderCommit: rf.commitIndex,
		}
		DPrintf("I'm %v, send appentry to %v, my log: %v, next[%v]:%v", rf.me, sever, rf.Logs, sever, rf.nextIndex[sever])
		if n > 0 && rf.Logs[n-1].Index >= next {
			pos, ok := rf.getLogPos(next)
			DPrintf("getpos ans: %v,%v", pos, ok)
			if next == 1 {
				args.PrevLogTerm = 0
			} else {
				if ok && n > 1 {
					args.PrevLogTerm = rf.Logs[pos-1].Term
				}
			}
			args.PrevLogIndex = next - 1
			args.Entries = rf.Logs[pos:]
		} else if n > 0 {
			args.PrevLogIndex = rf.Logs[n-1].Index
			args.PrevLogTerm = rf.Logs[n-1].Term
		}
		go func(server int, args AppendEntryArgs) {
			reply := AppendEntryReply{}
			ok := rf.sendAppendEntries(server, &args, &reply)
			if ok != false {
				rf.handleAppendEntriesReply(server, &reply)
			}
		}(i, args)
	}
}

func (rf *Raft) resetTimer() {
	timeOut := time.Duration(HeartBeatTime)
	if rf.state != LEADER {
		timeOut = time.Millisecond * time.Duration(ElectionMinTime+rand.Int63n(ElectionMaxTime-ElectionMinTime))
	}
	initchan := make(chan int, 1)
	if rf.timer == nil { //there is no timer, create it
		rf.timer = time.NewTimer(time.Millisecond * 5000)
		go func() {
			<-initchan
			for {
				<-rf.timer.C
				rf.handleTimer()
			}
		}()
	}
	rf.timer.Reset(timeOut)
	initchan <- 2
	//fmt.Println("Reset", rf.me, "timer, it's", rf.state, "dtime:", timeOut, "logs:", rf.Logs, "term:", rf.CurrentTerm)
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	//fmt.Print("start to init a raft node:  ", me, "      ")
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.applyCh = applyCh
	// Your initialization code here (2A, 2B, 2C).
	rf.me = me
	rf.CurrentTerm = 0
	rf.VotedFor = -1
	rf.Logs = []Log{}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.lastIncludeIndex = 0
	rf.lastIncludeTerm = 0

	rf.nextIndex = make([]int, len(peers))
	for i := range rf.nextIndex {
		rf.nextIndex[i] = 1
	}
	rf.matchIndex = make([]int, len(peers))
	rf.state = FOLLOWER
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	rf.persist()
	rf.resetTimer()

	return rf
}
