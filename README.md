# Distributed Chat Server Coordination System

A Go-based distributed coordination system implementing three fundamental distributed systems algorithms - **Raft Leader Election**, **Token Ring Mutual Exclusion**, and **Raft Log Replication** - applied to a distributed chat routing system. All inter-node communication uses Go's native **`net/rpc`** over TCP.

---

## Algorithms Implemented

### 1. Raft Leader Election
**File:** `internal/election/election.go`

| Concept | Implementation |
|---------|---------------|
| State Machine | Follower -> Candidate -> Leader |
| Failure Detection | Randomized heartbeat timeouts (300-500ms) |
| Voting | Term-based, first-come-first-served, majority quorum |
| Recovery | Automatic re-election on leader crash |

```
Follower --(timeout)--> Candidate --(majority vote)--> Leader
    ^                       |                              |
    |                       +---(higher term seen)---------+
    |                       |                       (sends heartbeats)
    +---(heartbeat rcvd)----+
```

### 2. Token Ring Mutual Exclusion
**File:** `internal/lock/lock.go`

| Concept | Implementation |
|---------|---------------|
| Topology | Logical ring: Node 0 -> Node 1 -> Node 2 -> Node 0 |
| Token | Single token circulates; only the holder enters the critical section |
| Fairness | Every node eventually receives the token (starvation-free) |
| No Deadlock | Token always circulates, even when idle |

```
        +------ Node 0 <------+
        |      (token?)        |
        v                      |
     Node 1 -------> Node 2 --+
```

### 3. Raft Log Replication (Consensus)
**File:** `internal/consensus/consensus.go`

| Concept | Implementation |
|---------|---------------|
| Leader appends | Entry added to leader's log |
| AppendEntries RPC | Sent to all followers in parallel |
| Majority commit | Entry committed once 2/3 nodes replicate it |
| State machine | Committed entries applied to the routing table |
| Safety | Log Matching, Leader Completeness (Raft sec 5.3) |

```
Leader           Follower 1        Follower 2
  |                  |                  |
  +--AppendEntries-->|                  |
  +--AppendEntries------------------------->|
  |                  |                  |
  |<--- ACK --------|                  |
  |<--- ACK --------------------------------|
  |                  |                  |
  |  (majority = 2/3 -> COMMITTED)     |
  |                  |                  |
  +--AppendEntries-->|  (commit index)  |
  +--AppendEntries------------------------->|
```

---

## Project Structure

```
+-- go.mod
+-- README.md
+-- internal/
|   +-- types/types.go                  # Shared types & message definitions
|   +-- transport/transport.go          # net/rpc service, server & client
|   +-- election/election.go            # Algorithm 1: Raft Leader Election
|   +-- lock/lock.go                    # Algorithm 2: Token Ring Mutual Exclusion
|   +-- consensus/consensus.go          # Algorithm 3: Raft Log Replication
|   +-- coordinator/coordinator.go      # Coordination ensemble node
|   +-- chatserver/chatserver.go        # Chat server (rooms, messages)
|   +-- client/client.go               # Chat client
+-- cmd/
|   +-- coordinator/main.go             # Coordinator CLI (multi-process RPC)
|   +-- chatserver/main.go              # Chat server launcher
|   +-- client/main.go                  # Client launcher
+-- demo/
|   +-- demo_full_chat/main.go          # * Full end-to-end chat demo (all 3 algos)
|   +-- demo_multi_node_rpc/main.go     # * Multi-node RPC over TCP demo
|   +-- demo_leader_failure/main.go     # Election & failure recovery demo
|   +-- demo_mutual_exclusion/main.go   # Token Ring contention demo
|   +-- demo_consensus/main.go          # Raft Log Replication demo
+-- scripts/
    +-- launch_cluster.ps1              # Launch 3-node cluster (Windows)
    +-- launch_cluster.sh               # Launch 3-node cluster (Linux/Mac)
```

---

## Quick Start

### Prerequisites
- **Go 1.21+** installed (https://go.dev/dl/)

### Build
```bash
go build ./...
```

### Run the Demos (Single Machine)

```bash
# * RECOMMENDED: Full end-to-end chat demo (all 3 algorithms + chat operations)
go run ./demo/demo_full_chat/

# * Multi-node RPC demo (3 nodes on separate TCP ports, real net/rpc)
go run ./demo/demo_multi_node_rpc/

# Individual algorithm demos:
go run ./demo/demo_leader_failure/       # Raft election + leader crash recovery
go run ./demo/demo_mutual_exclusion/     # Token Ring mutual exclusion
go run ./demo/demo_consensus/            # Raft Log Replication consensus
```

### Interactive CLI (Multi-Process on Same Machine)

```bash
# Terminal 1 - Node 0
go run ./cmd/coordinator/ -id=0 -addr=:7000 -peers="1=127.0.0.1:7001,2=127.0.0.1:7002"

# Terminal 2 - Node 1
go run ./cmd/coordinator/ -id=1 -addr=:7001 -peers="0=127.0.0.1:7000,2=127.0.0.1:7002"

# Terminal 3 - Node 2
go run ./cmd/coordinator/ -id=2 -addr=:7002 -peers="0=127.0.0.1:7000,1=127.0.0.1:7001"
```

Or use the script:
```powershell
.\scripts\launch_cluster.ps1    # Windows - opens 3 PowerShell windows
```

### CLI Commands (on any running coordinator)

| Command | Description |
|---------|-------------|
| `status` | Show election state (LEADER/FOLLOWER), term, leader ID |
| `lock <resource> <requester>` | Enter critical section via Token Ring |
| `unlock <resource> <requester>` | Exit critical section, pass token |
| `token` | Show token ring status (held/circulating) |
| `propose <txID> add_room <name> <server>` | Replicate room creation via Raft |
| `propose <txID> add_server <name> <addr>` | Replicate server registration via Raft |
| `state` | Show routing table & Raft log |
| `help` | Show all commands |
| `quit` | Shutdown node |

---

## Running on Multiple Laptops (Multi-Machine Setup)

This is the key demo for demonstrating real distributed communication across physical machines.

### Step 0: Prerequisites (all laptops)

1. **Go 1.21+** installed on all laptops
2. All connected to the **same WiFi / LAN network**
3. Copy the project folder to all laptops
4. Find each laptop's IP address:

```powershell
# Windows
ipconfig    # Look for "IPv4 Address" under your WiFi adapter
```
```bash
# Linux/Mac
ip addr     # or: ifconfig
```

**Example IPs (replace with yours):**
- **Laptop A** = `192.168.1.100`
- **Laptop B** = `192.168.1.101`

5. **Allow TCP ports 7000-7002** through the firewall:

```powershell
# Windows (run as Administrator):
New-NetFirewallRule -DisplayName "DS-CaseStudy" -Direction Inbound -Protocol TCP -LocalPort 7000-7002 -Action Allow
```
```bash
# Linux:
sudo ufw allow 7000:7002/tcp
```

### Step 1: Start the 3 Coordinator Nodes

We run **2 nodes on Laptop A** and **1 node on Laptop B** (totaling 3 nodes across 2 machines).

**Laptop A - Terminal 1 (Node 0):**
```bash
go run ./cmd/coordinator/ -id=0 -addr=:7000 -peers="1=192.168.1.100:7001,2=192.168.1.101:7002"
```

**Laptop A - Terminal 2 (Node 1):**
```bash
go run ./cmd/coordinator/ -id=1 -addr=:7001 -peers="0=192.168.1.100:7000,2=192.168.1.101:7002"
```

**Laptop B - Terminal 1 (Node 2):**
```bash
go run ./cmd/coordinator/ -id=2 -addr=:7002 -peers="0=192.168.1.100:7000,1=192.168.1.100:7001"
```

> **Wait ~2 seconds** - you will see election logs. One node becomes LEADER.

### Step 2: Demonstrate Raft Leader Election

Type `status` on each terminal:

**Output on the leader (e.g., Node 0):**
```
> status
  Node 0: State=LEADER, Term=1, KnownLeader=0
  This node IS the leader
```

**Output on a follower (e.g., Node 2 on Laptop B):**
```
> status
  Node 2: State=FOLLOWER, Term=1, KnownLeader=0
  This node is a follower
```

**What this shows:** Raft Leader Election elected a single leader across 2 physical machines using majority vote (2/3 votes needed).

### Step 3: Demonstrate Token Ring Mutual Exclusion

**On the LEADER's terminal:**
```
> lock General_Chat Server_A
  GRANTED critical section on "General_Chat" to [Server_A] (via Token Ring)

> lock General_Chat Server_B
> token
  Token: HELD - IN CRITICAL SECTION (resource: General_Chat, holder: Server_A)

> unlock General_Chat Server_A
  Released critical section on "General_Chat" (token passed)

> token
  Token: NOT HERE (circulating in ring)
```

**What this shows:** The Token Ring algorithm controls access - only the token holder can enter the critical section. After release, the token passes to the next node in the ring (even across machines).

### Step 4: Demonstrate Raft Log Replication (Consensus)

**On the LEADER's terminal:**
```
> propose tx-001 add_room Gaming_Lounge Server_Alpha:9001
  Proposing ADD_ROOM: Gaming_Lounge = Server_Alpha:9001...
  Raft Log Entry COMMITTED (majority replicated)

> propose tx-002 add_server Server_Beta 192.168.1.101:9002
  Proposing ADD_SERVER: Server_Beta = 192.168.1.101:9002...
  Raft Log Entry COMMITTED (majority replicated)

> state
  Routing Table (from Raft Log state machine):
     Room: "Gaming_Lounge" -> Server_Alpha:9001
     Server: "Server_Beta" -> 192.168.1.101:9002
  Raft Log: 2 entries, CommitIndex: 1
```

**Now, on Laptop B's terminal (the follower):**
```
> state
  Routing Table (from Raft Log state machine):
     Room: "Gaming_Lounge" -> Server_Alpha:9001
     Server: "Server_Beta" -> 192.168.1.101:9002
  Raft Log: 2 entries, CommitIndex: 1
```

**What this shows:** Both machines show the SAME routing table - the leader proposed entries, sent AppendEntries RPCs over the network, and both followers replicated and committed. This is Raft Log Replication working across machines.

### Step 5: Demonstrate Leader Failure & Recovery

**Kill the leader** (Ctrl+C on the leader's terminal).

**Wait ~1 second**, then type `status` on the surviving terminals:

```
> status
  Node 2: State=LEADER, Term=2, KnownLeader=2
  This node IS the leader
```

**What this shows:** The surviving nodes detected missing heartbeats, started a new election with a higher term, and elected a new leader - all automatically.

**The new leader can continue all operations:**
```
> propose tx-003 add_room Study_Group Server_Alpha:9001
  Raft Log Entry COMMITTED (majority replicated)
```

### Step 6: Full Chat Scenario (on the new leader)

```
> lock room_creation Coordinator
  GRANTED critical section on "room_creation" to [Coordinator]

> propose tx-004 add_room Study_Group Server_Alpha:9001
  Raft Log Entry COMMITTED

> unlock room_creation Coordinator
  Released critical section on "room_creation"

> state
  Routing Table:
     Room: "Gaming_Lounge" -> Server_Alpha:9001
     Room: "Study_Group" -> Server_Alpha:9001
     Server: "Server_Beta" -> 192.168.1.101:9002
```

---

## Demo Descriptions

### `demo_full_chat` - Full End-to-End Chat Demo (RECOMMENDED)
Runs through 7 phases demonstrating all algorithms in a real chat scenario:
1. **Cluster startup & Raft leader election** (3 nodes)
2. **Register chat servers** via Raft consensus (Server_Alpha, Server_Beta)
3. **Create chat rooms** using Token Ring mutex + Raft consensus (General_Chat, Gaming_Lounge)
4. **Users join rooms & send messages** (Alice, Bob, Charlie)
5. **Leader crashes -> automatic re-election** (Raft failure recovery)
6. **New leader continues operations** (creates Study_Group room)
7. **Concurrent room access** (Token Ring prevents conflicts)

```bash
go run ./demo/demo_full_chat/
```

### `demo_multi_node_rpc` - Multi-Node RPC Demo
Runs 3 coordinator nodes on separate TCP ports (7000, 7001, 7002), all communicating over Go's `net/rpc`. Shows the same algorithms working over real network calls instead of in-process channels.

```bash
go run ./demo/demo_multi_node_rpc/
```

### `demo_leader_failure` - Raft Election & Failure Recovery
Shows leader election, then kills the leader, and watches a new leader get elected.

### `demo_mutual_exclusion` - Token Ring Algorithm
Shows the token circulating, immediate grant, queued waiting, and release-and-pass.

### `demo_consensus` - Raft Log Replication
Shows log entry proposal, AppendEntries replication, majority commit, and routing table consistency across all nodes.

---

## Communication Architecture

All inter-node communication uses **Go's native `net/rpc`** package over TCP.

| RPC Method | Algorithm | Direction |
|-----------|-----------|-----------|
| `Node.RequestVote` | Raft Election | Candidate -> All |
| `Node.RespondVote` | Raft Election | Voter -> Candidate |
| `Node.SendHeartbeat` | Raft Election | Leader -> All |
| `Node.AppendEntries` | Raft Log Replication | Leader -> Followers |
| `Node.AppendEntriesReply` | Raft Log Replication | Follower -> Leader |
| `Node.PassToken` | Token Ring | Current holder -> Next in ring |

Demos use in-process channels for simplicity. The `cmd/coordinator` binary and `demo_multi_node_rpc` use real TCP/RPC.

---

*Built for DS Case Study - Distributed Systems, Semester 6*
